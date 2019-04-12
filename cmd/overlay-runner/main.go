package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/errors"
	"github.com/tinyci/ci-runners/runner"
	"github.com/tinyci/ci-runners/runner/config"
	"github.com/urfave/cli"
	"golang.org/x/sys/unix"
)

var hostname string

func init() {
	var err error
	hostname, err = os.Hostname()
	if err != nil {
		panic(errors.New(err).Wrap("could not get hostname"))
	}
}

func errOut(err interface{}) {
	fmt.Fprintf(os.Stderr, "Fatal Error during runner execution: %v\n", err)
	os.Exit(1)
}

func main() {
	app := cli.NewApp()
	app.Usage = "Run tinyci jobs with overlayfs and docker"
	app.Description = `
This runner provides a docker interface to running tinyci builds. It also
leverages an overlayfs backend and git cache to make clones fast.
`
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "config, c",
			Value: "/etc/tinyci/overlay-runner.yml",
			Usage: "Location of configuration file",
		},
	}

	app.Action = loop

	if err := app.Run(os.Args); err != nil {
		errOut(err)
	}
}

func handleCancel(r *runner.Run, cancel context.CancelFunc, cancelSig, runnerSig chan os.Signal) {
	<-cancelSig
	cancel()
retry:
	canceled, err := r.Config.Clients.Queue.GetCancel(r.QueueItem.Run.ID)
	if err != nil {
		fmt.Printf("Could not poll queuesvc; retrying in a second: %v\n", err)
		time.Sleep(time.Second)
		goto retry
	}

	if !canceled {
		if err := r.Config.Clients.Queue.SetCancel(r.QueueItem.Run.ID); err != nil {
			fmt.Printf("Cannot cancel current job, retrying in 1s: %v\n", err)
			time.Sleep(time.Second)
			goto retry
		}
	}

	fmt.Println("Signal received; will wait 10 seconds for cleanup to occur")
	time.Sleep(10 * time.Second)
	close(runnerSig)
}

func loop(ctx *cli.Context) error {
	// we reload the clients on each run
	c, err := config.Load(ctx.GlobalString("config"))
	if err != nil {
		errOut(err)
	}

	if c.Hostname == "" {
		c.Hostname = hostname
	}

	c.Clients.Log.WithFields(log.FieldMap{"hostname": c.Hostname}).Info("Initializing runner")

	runnerSig := make(chan os.Signal, 2)
	go func() {
		<-runnerSig
		c.Clients.Log.WithFields(log.FieldMap{"hostname": c.Hostname}).Info("Shutting down runner")
		os.Exit(0)
	}()
	signal.Notify(runnerSig, unix.SIGINT, unix.SIGTERM)

	for {
		qi, err := c.Clients.Queue.NextQueueItem(c.QueueName, c.Hostname)
		if err != nil {
			if !err.Contains(errors.ErrNotFound) {
				c.Clients.Log.Errorf("Error reading from queue: %v", err)
			}
			time.Sleep(time.Second)
			continue
		}

		fields := log.FieldMap{
			"hostname":   c.Hostname,
			"run_id":     fmt.Sprintf("%v", qi.Run.ID),
			"task_id":    fmt.Sprintf("%v", qi.Run.Task.ID),
			"parent":     qi.Run.Task.Parent.Name,
			"repository": qi.Run.Task.Ref.Repository.Name,
			"sha":        qi.Run.Task.Ref.SHA,
		}

		c.Clients.Log.WithFields(fields).Info("Received run data; commencing with test")

		since := time.Now()

		var (
			ctx    context.Context
			cancel context.CancelFunc
		)

		if qi.Run.RunSettings.Timeout != 0 {
			ctx, cancel = context.WithTimeout(context.Background(), qi.Run.RunSettings.Timeout)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}

		r := runner.NewRun(ctx, cancel, qi, c)

		cancelSig := make(chan os.Signal, 2)
		signal.Stop(runnerSig)

		signal.Notify(cancelSig, unix.SIGINT, unix.SIGTERM)
		go handleCancel(r, cancel, cancelSig, runnerSig)

		if err := r.RunDocker(); err != nil {
			c.Clients.Log.WithFields(fields).Errorf("Run concluded with error: %v", err)
		}

		signal.Notify(runnerSig, unix.SIGINT, unix.SIGTERM)

		didCancel, err := r.Config.Clients.Queue.GetCancel(r.QueueItem.Run.ID)
		if err != nil {
			c.Clients.Log.WithFields(fields).Errorf("Cannot retrieve cancel state of current job, retrying in 1s: %v\n", err)
			time.Sleep(time.Second)
		}

		if ctx.Err() == context.DeadlineExceeded && !didCancel {
			if err := r.Config.Clients.Queue.SetCancel(r.QueueItem.Run.ID); err != nil {
				c.Clients.Log.WithFields(fields).Errorf("Cannot cancel current job, retrying in 1s: %+v\n", err)
				time.Sleep(time.Second)
			}

			continue
		}

		if !didCancel {
		normalRetry:
			if err := c.Clients.Queue.SetStatus(qi.Run.ID, r.Status); err != nil {
				c.Clients.Log.WithFields(fields).Errorf("Status report resulted in error: %v", err)
				time.Sleep(time.Second)
				goto normalRetry
			}
		}

		c.Clients.Log.WithFields(fields).Infof("Run finished in %v", time.Since(since))
	}
}

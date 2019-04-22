package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/errors"
	fwConfig "github.com/tinyci/ci-runners/fw/config"
	sig "github.com/tinyci/ci-runners/fw/signal"
	"github.com/tinyci/ci-runners/fw/utils"
	runner "github.com/tinyci/ci-runners/runners/overlay-runner"
	"github.com/tinyci/ci-runners/runners/overlay-runner/config"
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
		utils.ErrOut(err)
	}
}

func loop(ctx *cli.Context) error {
	// we reload the clients on each run
	c := &config.Config{C: &fwConfig.Config{Clients: &fwConfig.Clients{}}}
	err := fwConfig.Load(ctx.GlobalString("config"), c)
	if err != nil {
		return err
	}

	if err := c.Runner.Validate(); err != nil {
		return err
	}

	if c.C.Hostname == "" {
		c.C.Hostname = hostname
	}

	c.C.Clients.Log = c.C.Clients.Log.WithFields(log.FieldMap{"hostname": c.C.Hostname})
	c.C.Clients.Log.Info("Initializing runner")

	runnerSig := make(chan os.Signal, 2)
	go func() {
		<-runnerSig
		c.C.Clients.Log.Info("Shutting down runner")
		os.Exit(0)
	}()
	signal.Notify(runnerSig, unix.SIGINT, unix.SIGTERM)

	for {
		qi, err := c.C.Clients.Queue.NextQueueItem(c.C.QueueName, c.C.Hostname)
		if err != nil {
			if !err.Contains(errors.ErrNotFound) {
				c.C.Clients.Log.Errorf("Error reading from queue: %v", err)
			}
			time.Sleep(time.Second)
			continue
		}

		fields := log.FieldMap{
			"hostname":   c.C.Hostname,
			"run_id":     fmt.Sprintf("%v", qi.Run.ID),
			"task_id":    fmt.Sprintf("%v", qi.Run.Task.ID),
			"parent":     qi.Run.Task.Parent.Name,
			"repository": qi.Run.Task.Ref.Repository.Name,
			"sha":        qi.Run.Task.Ref.SHA,
		}

		runLogger := c.C.Clients.Log.WithFields(fields)

		runLogger.Info("Received run data; commencing with test")

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

		r := runner.NewRun(ctx, cancel, qi, c, runLogger)

		cancelSig := make(chan os.Signal, 2)
		signal.Stop(runnerSig)

		sigCtx := &sig.Context{
			CancelFunc:   cancel,
			QueueClient:  c.C.Clients.Queue,
			RunID:        qi.Run.ID,
			CancelSignal: cancelSig,
			RunnerSignal: runnerSig,
			Done:         make(chan struct{}),
		}

		signal.Notify(cancelSig, unix.SIGINT, unix.SIGTERM)
		go sigCtx.HandleCancel()

		if err := r.RunDocker(); err != nil {
			runLogger.Errorf("Run concluded with error: %v", err)
		}

		close(sigCtx.Done)
		signal.Notify(runnerSig, unix.SIGINT, unix.SIGTERM)

		didCancel, err := r.Config.C.Clients.Queue.GetCancel(r.QueueItem.Run.ID)
		if err != nil {
			runLogger.Errorf("Cannot retrieve cancel state of current job, retrying in 1s: %v\n", err)
			time.Sleep(time.Second)
		}

		if ctx.Err() == context.DeadlineExceeded && !didCancel {
			if err := r.Config.C.Clients.Queue.SetCancel(r.QueueItem.Run.ID); err != nil {
				runLogger.Errorf("Cannot cancel current job, retrying in 1s: %+v\n", err)
				time.Sleep(time.Second)
			}

			continue
		}

		if !didCancel {
		normalRetry:
			if err := c.C.Clients.Queue.SetStatus(qi.Run.ID, r.Status); err != nil {
				runLogger.Errorf("Status report resulted in error: %v", err)
				time.Sleep(time.Second)
				goto normalRetry
			}
		}

		runLogger.Infof("Run finished in %v", time.Since(since))
	}
}

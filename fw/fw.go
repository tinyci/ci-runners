// Package fw is composed of a framework for designing tinyCI runner agents.
//
// Inside are several packages that cover config files, signals, and other
// features. You will want to read the docs for them too.
//
// To implement a runner, one must satisfy the Runner interface. Then, store it
// in an Entrypoint with your other runner info, and call Run(Entrypoint). The
// rest of the system will automate the process of:
//
//		* Coordinating and starting your run
//		* Managing signals and cancellations
//		* Logging certain functionality
//
// What you do outside of that is *up to you*. The runner framework is
// deliberately light to avoid prescribing ways runners should work.
//
package fw

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/errors"
	"github.com/tinyci/ci-agents/model"
	sig "github.com/tinyci/ci-runners/fw/signal"
	"github.com/urfave/cli"
	"golang.org/x/sys/unix"
)

// Runner is the interface that a runner must implement to leverage this
// framework.
type Runner interface {

	//
	// Lifecycle hooks
	//
	// Init is the entrypoint of the runner application and will be run shortly
	// after command line arguments are processed.
	Init(*Context) *errors.Error
	// BeforeNextRun is executed to set up the run but not actually execute it.
	BeforeNextRun(*Context) *errors.Error
	// Run is the actual running of the job. Errors from contexts are handled as
	// cancellations. The status (pass/fail) is returned as the primary value.
	Run(*Context) (bool, *errors.Error)

	//
	// Data calls
	//
	// QueueName is the name of the queue to pull runs off of.
	QueueName() string
	// Hostname is the name of the host; a tag to uniquely identify it.
	Hostname() string

	//
	// Client acquisition
	//
	// QueueClient is a client to the queuesvc.
	QueueClient() *queue.Client
	// LogsvcClient is a client to the logsvc.
	LogsvcClient(*Context) *log.SubLogger
}

// Entrypoint is composed of boot-time entities used to start up the
// application, such as the argument parser and Runner object to enter.
type Entrypoint struct {
	// Usage is the way to use the runner application.
	Usage string
	// Description is an extended description of what the runner does and how it works.
	Description string
	// Version is the version of the runner program.
	Version string
	// Author is the author of the runner program.
	Author string
	// Flags are any extra flags you want to handle. We use urfave/cli for managing flags.
	Flags []cli.Flag
	// TeardownTime is the amount of time to wait for the runner to tear down
	// everything so it can exit.
	TeardownTime time.Duration
	// Launch is the Runner intended to be executed.
	Launch Runner
}

// Context is the set of data generated from the runner framework controller
// code, but formatted to be sent to implementing hooks of the Runner
// interface.
//
// Please note that not all situations will call for all data to be
// represented, so check your struct values (or the docs) before using things.
type Context struct {
	// QueueItem is the item of the upcoming run; used in BeforeNextRun() and Run()
	QueueItem *model.QueueItem
	// RunStart is the time the run started. Populated only for Run().
	RunStart time.Time
	// RunCtx is the context.Context for the run; if closed the run should be canceled.
	RunCtx context.Context
	// RunCancelFunc is the cancel func to close the above context.
	RunCancelFunc context.CancelFunc

	// CLIContext is the urfave/cli.Context for managing CLI flags and other
	// functionality.
	CLIContext *cli.Context
}

// Run runs the given Entrypoint, which should contain a Runner to launch as
// well as other information about the runner.  On error you can assume the
// only safe option is to exit.
//
// At the time of this call, arguments will be parsed. Avoid parsing arguments
// before this call.
func Run(e Entrypoint) error {
	app := cli.NewApp()
	app.Usage = e.Usage
	app.Description = e.Description
	app.Version = e.Version
	app.Author = e.Author
	app.Flags = append(e.Flags, cli.StringFlag{
		Name:  "config, c",
		Value: "/etc/tinyci/runner.yml",
		Usage: "Location of configuration file",
	})

	app.Action = e.loop()

	return app.Run(os.Args)
}

func (e *Entrypoint) loop() func(ctx *cli.Context) error {
	runner := e.Launch
	return func(ctx *cli.Context) error {
		baseContext := &Context{CLIContext: ctx}
		if err := runner.Init(baseContext); err != nil {
			return err
		}

		runnerSignal := make(chan os.Signal, 2)
		go func() {
			<-runnerSignal
			runner.LogsvcClient(baseContext).Info("Shutting down runner")
			os.Exit(0)
		}()
		signal.Notify(runnerSignal, unix.SIGINT, unix.SIGTERM)

		runner.LogsvcClient(baseContext).Info("Initializing runner")

		for {
			qi, err := runner.QueueClient().NextQueueItem(runner.QueueName(), runner.Hostname())
			if err != nil {
				if !err.Contains(errors.ErrNotFound) {
					runner.LogsvcClient(baseContext).Errorf("Error reading from queue: %v", err)
				}
				time.Sleep(time.Second)
				continue
			}

			runnerCtx := &Context{QueueItem: qi, RunStart: time.Now()}
			runLogger := runner.LogsvcClient(runnerCtx)

			runLogger.Info("Received run data; commencing with test")
			if qi.Run.RunSettings.Timeout != 0 {
				runnerCtx.RunCtx, runnerCtx.RunCancelFunc = context.WithTimeout(context.Background(), qi.Run.RunSettings.Timeout)
			} else {
				runnerCtx.RunCtx, runnerCtx.RunCancelFunc = context.WithCancel(context.Background())
			}

			cancelSig := make(chan os.Signal, 2)
			signal.Stop(runnerSignal)

			sigCtx := &sig.Context{
				CancelFunc:   runnerCtx.RunCancelFunc,
				QueueClient:  runner.QueueClient(),
				RunID:        qi.Run.ID,
				CancelSignal: cancelSig,
				RunnerSignal: runnerSignal,
				Done:         make(chan struct{}),
			}

			signal.Notify(cancelSig, unix.SIGINT, unix.SIGTERM)
			go sigCtx.HandleCancel(e.TeardownTime)

			if err := runner.BeforeNextRun(runnerCtx); err != nil {
				return err
			}

			status, err := runner.Run(runnerCtx)
			if err != nil {
				runLogger.Errorf("Run concluded with FATAL ERROR: %v", err)
				return err
			}

			close(sigCtx.Done)
			signal.Notify(runnerSignal, unix.SIGINT, unix.SIGTERM)

			didCancel, err := runner.QueueClient().GetCancel(qi.Run.ID)
			if err != nil {
				runLogger.Errorf("Cannot retrieve cancel state of current job, retrying in 1s: %v\n", err)
				time.Sleep(time.Second)
			}

			if runnerCtx.RunCtx.Err() == context.DeadlineExceeded && !didCancel {
				if err := runner.QueueClient().SetCancel(qi.Run.ID); err != nil {
					runLogger.Errorf("Cannot cancel current job, retrying in 1s: %+v\n", err)
					time.Sleep(time.Second)
				}

				continue
			}

			if !didCancel {
			normalRetry:
				if err := runner.QueueClient().SetStatus(qi.Run.ID, status); err != nil {
					runLogger.Errorf("Status report resulted in error: %v", err)
					time.Sleep(time.Second)
					goto normalRetry
				}
			}

			runLogger.Infof("Run finished in %v", time.Since(runnerCtx.RunStart))
		}
	}
}

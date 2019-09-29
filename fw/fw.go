// Package fw is composed of a framework for designing tinyCI runner agents.
//
// Inside this directory as subdirectories are several packages that cover
// config files, signals, and other features that are documented and also
// optional in most cases.
//
// To implement a runner, it must contain a struct that must satisfy the Runner
// interface. Then, an Entrypoint must be created with metadata about the
// runner, and the runner struct itself. Then, Run(Entrypoint) needs to be
// called. The rest of the system will automate the process of:
//
//		* Coordinating and starting your run
//		* Managing signals and cancellations
//		* Logging certain functionality
//
// What is done outside of these framework needs is largely irrelevant to the
// framework itself.
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
	// BeforeRun is executed to set up the run but not actually execute it.
	BeforeRun(*Context) *errors.Error
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
	// TeardownTimeout is the amount of time to wait for the runner to tear down
	// everything so it can exit.
	TeardownTimeout time.Duration
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
	launchCtx, launchCancel := context.WithCancel(context.Background())
	return func(ctx *cli.Context) error {
		baseContext := &Context{CLIContext: ctx}
		if err := runner.Init(baseContext); err != nil {
			return err
		}

		runnerSignal := make(chan os.Signal, 2)
		go func() {
			<-runnerSignal
			runner.LogsvcClient(baseContext).Info(context.Background(), "Shutting down runner")
			launchCancel()
			os.Exit(0)
		}()
		signal.Notify(runnerSignal, unix.SIGINT, unix.SIGTERM)

		runner.LogsvcClient(baseContext).Info(launchCtx, "Initializing runner")

		for {
			qi, err := runner.QueueClient().NextQueueItem(launchCtx, runner.QueueName(), runner.Hostname())
			if err != nil {
				if !err.Contains(errors.ErrNotFound) {
					runner.LogsvcClient(baseContext).Errorf(launchCtx, "Error reading from queue: %v", err)
				}
				if err.Contains(context.Canceled) {
					os.Exit(0)
				}
				time.Sleep(time.Second)
				continue
			}

			runnerCtx := &Context{QueueItem: qi, RunStart: time.Now()}
			runLogger := runner.LogsvcClient(runnerCtx)

			runLogger.Info(context.Background(), "Received run data; commencing with test")
			if qi.Run.RunSettings.Timeout != 0 {
				runnerCtx.RunCtx, runnerCtx.RunCancelFunc = context.WithTimeout(context.Background(), qi.Run.RunSettings.Timeout)
			} else {
				runnerCtx.RunCtx, runnerCtx.RunCancelFunc = context.WithCancel(context.Background())
			}

			cancelSig := make(chan os.Signal, 2)
			signal.Stop(runnerSignal)

			sigCtx := &sig.Context{
				LaunchCancel: launchCancel,
				CancelFunc:   runnerCtx.RunCancelFunc,
				QueueClient:  runner.QueueClient(),
				RunID:        qi.Run.ID,
				Context:      runnerCtx.RunCtx,
				CancelSignal: cancelSig,
				RunnerSignal: runnerSignal,
				Done:         make(chan struct{}),
			}

			signal.Notify(cancelSig, unix.SIGINT, unix.SIGTERM)
			go sigCtx.HandleCancel(e.TeardownTimeout)

			if err := runner.BeforeRun(runnerCtx); err != nil {
				return err
			}

			status, err := runner.Run(runnerCtx)
			if err != nil {
				runLogger.Errorf(runnerCtx.RunCtx, "Run concluded with FATAL ERROR: %v", err)
				return err
			}

			close(sigCtx.Done)
			signal.Stop(cancelSig)
			signal.Notify(runnerSignal, unix.SIGINT, unix.SIGTERM)

			didCancel, err := runner.QueueClient().GetCancel(context.Background(), qi.Run.ID)
			if err != nil {
				runLogger.Errorf(context.Background(), "Cannot retrieve cancel state of current job, retrying in 1s: %v\n", err)
				time.Sleep(time.Second)
			}

			if runnerCtx.RunCtx.Err() == context.DeadlineExceeded && !didCancel {
				if err := runner.QueueClient().SetCancel(context.Background(), qi.Run.ID); err != nil {
					runLogger.Errorf(context.Background(), "Cannot cancel current job, retrying in 1s: %+v\n", err)
					time.Sleep(time.Second)
				}

				continue
			}

			if !didCancel {
			normalRetry:
				if err := runner.QueueClient().SetStatus(context.Background(), qi.Run.ID, status); err != nil {
					runLogger.Errorf(context.Background(), "Status report resulted in error: %v", err)
					time.Sleep(time.Second)
					goto normalRetry
				}
			}

			runLogger.Infof(context.Background(), "Run finished in %v", time.Since(runnerCtx.RunStart))
		}
	}
}

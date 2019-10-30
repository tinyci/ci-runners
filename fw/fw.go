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
	"sync"
	"time"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/errors"
	fwcontext "github.com/tinyci/ci-runners/fw/context"
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
	Init(*fwcontext.Context) *errors.Error
	// BeforeRun is executed to set up the run but not actually execute it.
	BeforeRun(*fwcontext.RunContext) *errors.Error
	// Run is the actual running of the job. Errors from contexts are handled as
	// cancellations. The status (pass/fail) is returned as the primary value.
	Run(*fwcontext.RunContext) (bool, *errors.Error)

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
	LogsvcClient(*fwcontext.RunContext) *log.SubLogger
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

	terminate      bool
	terminateMutex sync.RWMutex
}

// Run runs the given Entrypoint, which should contain a Runner to launch as
// well as other information about the runner.  On error you can assume the
// only safe option is to exit.
//
// At the time of this call, arguments will be parsed. Avoid parsing arguments
// before this call.
func Run(e *Entrypoint) error {
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

func (e *Entrypoint) getTerminate() bool {
	e.terminateMutex.RLock()
	defer e.terminateMutex.RUnlock()

	return e.terminate
}

// SetTerminate tells the runner to terminate at the end of the next iteration
func (e *Entrypoint) SetTerminate(log *log.SubLogger) {
	e.terminateMutex.Lock()
	defer e.terminateMutex.Unlock()
	e.terminate = true
}

func (e *Entrypoint) loop() func(*cli.Context) error {
	runner := e.Launch
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())

	return func(ctx *cli.Context) error {
		baseContext := &fwcontext.Context{CLIContext: ctx}
		if err := runner.Init(baseContext); err != nil {
			return err
		}

		log := runner.LogsvcClient(&fwcontext.RunContext{Context: baseContext})
		log.Info(lifetimeCtx, "Initializing runner")

		e.makeGracefulRestartSignal(lifetimeCtx, log)

		for {
			if err := e.iterate(lifetimeCtx, lifetimeCancel, baseContext, runner); err != nil {
				return err
			}
			if e.getTerminate() {
				log.Info(lifetimeCtx, "Termination requested after the end of the run")
				return nil
			}
		}
	}
}

func (e *Entrypoint) makeGracefulRestartSignal(ctx context.Context, log *log.SubLogger) {
	sigChan := make(chan os.Signal, 1)

	go func() {
		for range sigChan {
			log.Info(ctx, "Termination requested at the end of any outstanding run")
			e.SetTerminate(log)
		}
	}()

	signal.Notify(sigChan, unix.SIGHUP)
}

func (e *Entrypoint) makeRunnerSignal(lifetimeCancel context.CancelFunc, log *log.SubLogger, runnerCtx *fwcontext.RunContext) chan os.Signal {
	runnerSignal := make(chan os.Signal, 2)
	go func() {
		for sig := range runnerSignal {
			switch sig {
			case unix.SIGINT, unix.SIGTERM:
				if runnerCtx != nil {
					e.processCancel(context.Background(), runnerCtx, e.Launch)
				}
				lifetimeCancel()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				log.Info(ctx, "Shutting down runner")
				cancel()
				os.Exit(0)
			}
		}
	}()
	signal.Notify(runnerSignal, unix.SIGINT, unix.SIGTERM)

	return runnerSignal
}

func (e *Entrypoint) processCancel(ctx context.Context, runnerCtx *fwcontext.RunContext, runner Runner) bool {
retry:
	runLogger := runner.LogsvcClient(runnerCtx)
	didCancel, err := runner.QueueClient().GetCancel(ctx, runnerCtx.QueueItem.Run.ID)
	if err != nil {
		runLogger.Errorf(ctx, "Cannot retrieve cancel state of current job, retrying in 1s: %v\n", err)
		time.Sleep(time.Second)
	}

	if runnerCtx.Ctx.Err() == context.DeadlineExceeded && !didCancel {
		if err := runner.QueueClient().SetCancel(ctx, runnerCtx.QueueItem.Run.ID); err != nil {
			runLogger.Errorf(ctx, "Cannot cancel current job, retrying in 1s: %+v\n", err)
			time.Sleep(time.Second)
		}

		goto retry
	}

	return didCancel
}

func (e *Entrypoint) respondToCancelSignal(runnerCtx *fwcontext.RunContext) {
	for {
		select {
		case <-runnerCtx.Ctx.Done():
			return
		default:
			cancel, _ := e.Launch.QueueClient().GetCancel(runnerCtx.Ctx, runnerCtx.QueueItem.Run.ID)
			if cancel {
				if err := e.Launch.QueueClient().SetCancel(runnerCtx.Ctx, runnerCtx.QueueItem.Run.ID); err != nil {
					time.Sleep(time.Second)
					continue
				}
			}
		}
	}
}

func (e *Entrypoint) iterate(ctx context.Context, cancel context.CancelFunc, baseContext *fwcontext.Context, runner Runner) error {
	log := runner.LogsvcClient(&fwcontext.RunContext{Context: baseContext})
	runnerSignal := e.makeRunnerSignal(cancel, log, nil)

	qi, err := runner.QueueClient().NextQueueItem(ctx, runner.QueueName(), runner.Hostname())
	if err != nil {
		if !err.Contains(errors.ErrNotFound) {
			log.Errorf(ctx, "Error reading from queue: %v", err)
		}
		if err.Contains(context.Canceled) {
			e.SetTerminate(log)
		} else {
			time.Sleep(time.Second)
		}
		return nil
	}

	runnerCtx := &fwcontext.RunContext{QueueItem: qi, Start: time.Now(), Context: baseContext}
	runLogger := runner.LogsvcClient(runnerCtx)
	runLogger.Info(ctx, "Received run data; commencing with test")
	timeout := qi.Run.RunSettings.Timeout

	if timeout == 0 {
		runnerCtx.Ctx, runnerCtx.CancelFunc = context.WithCancel(context.Background())
	} else {
		runnerCtx.Ctx, runnerCtx.CancelFunc = context.WithTimeout(context.Background(), qi.Run.RunSettings.Timeout)
	}

	sigCtx := &SignalContext{
		QueueClient: runner.QueueClient(),
		RunContext:  runnerCtx,
		Ctx:         ctx,
		Log:         runLogger,
		Channel:     make(chan os.Signal, 2),
		Entrypoint:  e,
	}

	go sigCtx.HandleCancel()
	signal.Stop(runnerSignal)
	signal.Notify(sigCtx.Channel, unix.SIGINT, unix.SIGTERM)

	go e.respondToCancelSignal(runnerCtx)

	if err := runner.BeforeRun(runnerCtx); err != nil {
		return err
	}

	status, err := runner.Run(runnerCtx)
	if err != nil {
		runLogger.Errorf(ctx, "Run concluded with FATAL ERROR: %v", err)
		return err
	}
	runnerSignal = e.makeRunnerSignal(cancel, log, runnerCtx)
	defer signal.Stop(runnerSignal)
	signal.Stop(sigCtx.Channel)

	if e.processCancel(ctx, runnerCtx, runner) {
		return nil
	}

normalRetry:
	if err := runner.QueueClient().SetStatus(ctx, qi.Run.ID, status); err != nil {
		runLogger.Errorf(ctx, "Status report resulted in error: %v", err)
		time.Sleep(time.Second)
		goto normalRetry
	}

	runLogger.Infof(ctx, "Run finished in %v", time.Since(runnerCtx.Start))
	return nil
}

// SignalContext is the context in which the handlers run under; they will be used to
// store clients as well as data about the run.
//
// Creating a new one of these for each run is a necessity.
type SignalContext struct {
	// QueueClient is a client to the queuesvc
	QueueClient *queue.Client
	// RunContext is the run context
	RunContext *fwcontext.RunContext

	Ctx context.Context

	Log     *log.SubLogger
	Channel chan os.Signal

	Entrypoint *Entrypoint
}

// HandleCancel allows the user to program the queuesvc with a cancellation
// when a signal is received, and simultaneously trigger a cancellation of the
// run.
//
// To use this function, populate the *Context as described; then call this in a
// goroutine. Once triggered, it is assuming the daemon is shutting down and
// will trigger all cancellation behavior in the context.
func (sigctx *SignalContext) HandleCancel() {
	for range sigctx.Channel {
		sigctx.Entrypoint.SetTerminate(sigctx.Log)
	retry:
		canceled, err := sigctx.QueueClient.GetCancel(sigctx.Ctx, sigctx.RunContext.QueueItem.Run.ID)
		if err != nil {
			sigctx.Log.Errorf(sigctx.Ctx, "Could not poll queuesvc; retrying in a second: %v\n", err)
			time.Sleep(time.Second)
			goto retry
		}

		if !canceled {
			if err := sigctx.QueueClient.SetCancel(sigctx.Ctx, sigctx.RunContext.QueueItem.Run.ID); err != nil {
				sigctx.Log.Errorf(sigctx.Ctx, "Cannot cancel current job, retrying in 1s: %v\n", err)
				time.Sleep(time.Second)
				goto retry
			}
		}

		sigctx.Log.Errorf(sigctx.Ctx, "Signal received; will wait %v for cleanup to occur\n", sigctx.Entrypoint.TeardownTimeout)
		time.Sleep(sigctx.Entrypoint.TeardownTimeout)
	}
}

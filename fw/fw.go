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
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	fwcontext "github.com/tinyci/ci-runners/fw/context"
	"github.com/urfave/cli"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type runMap map[Run]*fwcontext.RunContext

// Runner is the interface that a runner must implement to leverage this
// framework.
type Runner interface {

	// Init is the entrypoint of the runner application and will be run shortly
	// after command line arguments are processed.
	Init(*fwcontext.Context) error

	// MakeRun allows the user to customize the run before returning it. See the
	// `Run` interface.
	MakeRun(string, *fwcontext.RunContext) (Run, error)

	// AfterRun executes after the run has been completed.
	AfterRun(string, *fwcontext.RunContext)

	// Ready just indicates when the runner is ready for another queue item
	Ready() bool

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

// Run is the lifecycle of a single run.
type Run interface {
	fmt.Stringer

	// Name is the name of the run
	Name() string

	// RunContext returns the *fwcontext.RunContext used to create this run.
	RunContext() *fwcontext.RunContext

	//
	// Lifecycle hooks
	//

	// BeforeRun is executed to set up the run but not actually execute it.
	BeforeRun() error

	// Run is the actual running of the job. Errors from contexts are handled as
	// cancellations. The status (pass/fail) is returned as the primary value.
	Run() (bool, error)

	// AfterRun is executed after the run has completed.
	AfterRun() error
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

	runMap      runMap
	runMapMutex sync.RWMutex
}

// Launch runs the given Entrypoint, which should contain a Runner to launch as
// well as other information about the runner.  On error you can assume the
// only safe option is to exit.
//
// At the time of this call, arguments will be parsed. Avoid parsing arguments
// before this call.
func Launch(e *Entrypoint) error {
	e.runMap = runMap{}

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

		e.makeGracefulRestartSignal(lifetimeCancel, log)

		for {
			if err := e.iterate(lifetimeCtx, lifetimeCancel, baseContext, runner); err != nil {
				return err
			}
		}
	}
}

func (e *Entrypoint) makeGracefulRestartSignal(lifetimeCancel context.CancelFunc, log *log.SubLogger) {
	sigChan := make(chan os.Signal, 1)

	go func() {
		for sig := range sigChan {
			switch sig {
			case unix.SIGINT, unix.SIGTERM:
				wg := &sync.WaitGroup{}
				e.runMapMutex.Lock() // will hold until exit
				wg.Add(len(e.runMap))
				for run, runnerCtx := range e.runMap {
					go func(run Run, runnerCtx *fwcontext.RunContext, wg *sync.WaitGroup) {
						defer wg.Done()
						e.processCancel(context.Background(), runnerCtx, e.Launch)
					}(run, runnerCtx, wg)
				}
				wg.Wait()
				lifetimeCancel()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				log.Info(ctx, "Shutting down runner")
				cancel()
				os.Exit(0)
			case unix.SIGHUP:
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				log.Info(ctx, "Termination requested at the end of any outstanding run")
				cancel()
				e.SetTerminate(log)
			}
		}
	}()

	signal.Notify(sigChan, unix.SIGHUP, unix.SIGINT, unix.SIGTERM)
}

func (e *Entrypoint) processCancel(ctx context.Context, runnerCtx *fwcontext.RunContext, runner Runner) bool {
retry:
	runLogger := runner.LogsvcClient(runnerCtx)
	didCancel, err := runner.QueueClient().GetCancel(ctx, runnerCtx.QueueItem.Run.Id)
	if err != nil {
		runLogger.Errorf(ctx, "Cannot retrieve cancel state of current job, retrying in 1s: %v\n", err)
		time.Sleep(time.Second)
	}

	if !didCancel {
		runLogger.Info(ctx, "Canceling run")
		if err := runner.QueueClient().SetCancel(context.Background(), runnerCtx.QueueItem.Run.Id); err != nil {
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
			cancel, _ := e.Launch.QueueClient().GetCancel(runnerCtx.Ctx, runnerCtx.QueueItem.Run.Id)
			if cancel && runnerCtx.CancelFunc != nil {
				runnerCtx.CancelFunc()
			}
			time.Sleep(time.Second)
		}
	}
}

func (e *Entrypoint) iterate(ctx context.Context, cancel context.CancelFunc, baseContext *fwcontext.Context, runner Runner) error {
	log := runner.LogsvcClient(&fwcontext.RunContext{Context: baseContext})

	e.runMapMutex.RLock()
	count := uint(len(e.runMap))
	e.runMapMutex.RUnlock()

	if count == 0 && e.getTerminate() {
		log.Info(ctx, "Termination requested after the end of the run")
		os.Exit(0)
	}

	if e.getTerminate() || !runner.Ready() {
		time.Sleep(time.Second)
		return nil
	}

	qi, err := runner.QueueClient().NextQueueItem(ctx, runner.QueueName(), runner.Hostname())
	if err != nil {
		if stat, ok := status.FromError(err); ok && stat.Code() == codes.NotFound {
			return nil
		}

		if stat, ok := status.FromError(err); ok && stat.Code() != codes.NotFound {
			log.Errorf(ctx, "Error reading from queue: %v", err)
			time.Sleep(time.Second)
		}

		select {
		case <-ctx.Done():
			e.SetTerminate(log)
		default:
		}

		return nil
	}

	runnerCtx := &fwcontext.RunContext{QueueItem: qi, Start: time.Now(), Context: baseContext}
	runLogger := runner.LogsvcClient(runnerCtx)
	runLogger.Info(ctx, "Received run data; commencing with test")
	timeout := qi.Run.Settings.Timeout

	if timeout == 0 {
		runnerCtx.Ctx, runnerCtx.CancelFunc = context.WithCancel(context.Background())
	} else {
		runnerCtx.Ctx, runnerCtx.CancelFunc = context.WithTimeout(context.Background(), time.Duration(qi.Run.Settings.Timeout))
	}

	runName := strings.Join([]string{runner.QueueName(), fmt.Sprintf("%d", qi.Run.Id)}, ".")

	run, err := runner.MakeRun(runName, runnerCtx)
	if err != nil {
		return err
	}

	e.runMapMutex.Lock()
	e.runMap[run] = runnerCtx
	e.runMapMutex.Unlock()

	go e.respondToCancelSignal(runnerCtx)

	go func() {
		defer func() {
			runLogger.Infof(ctx, "Run finished in %v", time.Since(runnerCtx.Start))

			e.runMapMutex.Lock()
			delete(e.runMap, run)
			e.runMapMutex.Unlock()

			runner.AfterRun(runName, runnerCtx)
		}()

		if err := run.BeforeRun(); err != nil {
			runLogger.Errorf(ctx, "Run configuration errored: %v", err)
			return
		}

		status, err := run.Run()
		if err != nil {
			runLogger.Errorf(ctx, "Run concluded with error: %v", err)
		}

		if err := run.AfterRun(); err != nil {
			runLogger.Errorf(ctx, "AfterRun hook failed with error: %v", err)
		}

	normalRetry:
		cancel, err := e.Launch.QueueClient().GetCancel(ctx, runnerCtx.QueueItem.Run.Id)
		if err != nil {
			runLogger.Errorf(ctx, "Cancel check resulted in error: %v", err)
			time.Sleep(time.Second)

			goto normalRetry
		}

		if !cancel {
			if err := runner.QueueClient().SetStatus(ctx, qi.Run.Id, status); err != nil {
				// FIXME this should be a *constant*
				if !strings.Contains(err.Error(), "status already set for run") {
					runLogger.Errorf(ctx, "Status report resulted in error: %v", err)
					time.Sleep(time.Second)

					goto normalRetry
				}
			}
		}
	}()

	return nil
}

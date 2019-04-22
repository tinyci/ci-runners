package runner

import (
	"fmt"
	"os"

	"github.com/tinyci/ci-agents/clients/log"
	logsvc "github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/errors"
	"github.com/tinyci/ci-runners/fw"
	fwConfig "github.com/tinyci/ci-runners/fw/config"
	"github.com/tinyci/ci-runners/runners/overlay-runner/config"
)

// Runner encapsulates an infinite lifecycle overlay-runner.
type Runner struct {
	Config     *config.Config
	CurrentRun *Run
}

// Init is the bootstrap of the runner.
func (r *Runner) Init(ctx *fw.Context) *errors.Error {
	// we reload the clients on each run
	r.Config = &config.Config{C: fwConfig.Config{Clients: &fwConfig.Clients{}}}
	err := fwConfig.Load(ctx.CLIContext.GlobalString("config"), r.Config)
	if err != nil {
		return err
	}

	if err := r.Config.Runner.Validate(); err != nil {
		return err
	}

	if r.Config.C.Hostname == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return errors.New(err).Wrap("Could not retrieve hostname")
		}
		r.Config.C.Hostname = hostname
	}

	r.Config.C.Clients.Log = r.Config.C.Clients.Log.WithFields(log.FieldMap{"hostname": r.Config.C.Hostname})

	return nil
}

// BeforeRun is executed before the next run is started.
func (r *Runner) BeforeRun(ctx *fw.Context) *errors.Error {
	r.CurrentRun = NewRun(ctx.RunCtx, ctx.RunCancelFunc, ctx.QueueItem, r.Config, r.LogsvcClient(ctx))
	return nil
}

// Run runs the CI job.
func (r *Runner) Run(ctx *fw.Context) (bool, *errors.Error) {
	if err := r.CurrentRun.RunDocker(); err != nil {
		r.LogsvcClient(ctx).Errorf("Run concluded with error: %v", err)
	}

	defer func() { r.CurrentRun = nil }()
	return r.CurrentRun.Status, nil
}

// Hostname is the reported hostname of the machine; an identifier. Not
// necessary for anything and insecure, just ornamental.
func (r *Runner) Hostname() string {
	return r.Config.C.Hostname
}

// QueueName is the name of the queue this runner should be processing.
func (r *Runner) QueueName() string {
	return r.Config.C.QueueName
}

// QueueClient returns the queue client
func (r *Runner) QueueClient() *queue.Client {
	return r.Config.C.Clients.Queue
}

// LogsvcClient returns the system log client. Must be called after configuration is initialized
func (r *Runner) LogsvcClient(ctx *fw.Context) *log.SubLogger {
	log := r.Config.C.Clients.Log.WithFields(log.FieldMap{"hostname": r.Config.C.Hostname})

	if ctx.QueueItem != nil {
		return log.WithFields(logsvc.FieldMap{
			"run_id":     fmt.Sprintf("%v", ctx.QueueItem.Run.ID),
			"task_id":    fmt.Sprintf("%v", ctx.QueueItem.Run.Task.ID),
			"parent":     ctx.QueueItem.Run.Task.Parent.Name,
			"repository": ctx.QueueItem.Run.Task.Ref.Repository.Name,
			"sha":        ctx.QueueItem.Run.Task.Ref.SHA,
		})
	}

	return log
}

package runner

import (
	"fmt"
	"os"

	"github.com/docker/docker/client"
	"github.com/tinyci/ci-agents/clients/log"
	logsvc "github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/errors"
	fwConfig "github.com/tinyci/ci-runners/fw/config"
	fwcontext "github.com/tinyci/ci-runners/fw/context"
	"github.com/tinyci/ci-runners/runners/overlay-runner/config"
)

// Runner encapsulates an infinite lifecycle overlay-runner.
type Runner struct {
	Config     *config.Config
	CurrentRun *Run
	Docker     *client.Client
}

// Init is the bootstrap of the runner.
func (r *Runner) Init(ctx *fwcontext.Context) error {
	// we reload the clients on each run
	r.Config = &config.Config{C: fwConfig.Config{Clients: &fwConfig.Clients{}}}
	err := fwConfig.Load(ctx.CLIContext.GlobalString("config"), r.Config)
	if err != nil {
		return err
	}

	if err := r.Config.Runner.Validate(); err != nil {
		return err
	}

	var eErr error
	r.Docker, eErr = client.NewClientWithOpts(client.FromEnv)
	if eErr != nil {
		return errors.New(eErr)
	}

	if r.Config.C.Hostname == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return errors.New(err).(errors.Error).Wrap("Could not retrieve hostname")
		}
		r.Config.C.Hostname = hostname
	}

	r.Config.C.Clients.Log = r.Config.C.Clients.Log.WithFields(log.FieldMap{"hostname": r.Config.C.Hostname})

	return nil
}

// BeforeRun is executed before the next run is started.
func (r *Runner) BeforeRun(ctx *fwcontext.RunContext) error {
	var err error
	r.CurrentRun, err = NewRun(ctx.Ctx, ctx.CancelFunc, ctx.QueueItem, r.Config, r.LogsvcClient(ctx), r.Docker)
	return err
}

// Run runs the CI job.
func (r *Runner) Run(ctx *fwcontext.RunContext) (bool, error) {
	if err := r.CurrentRun.RunDocker(); err != nil {
		r.LogsvcClient(ctx).Errorf(ctx.Ctx, "Run concluded with error: %v", err)
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
func (r *Runner) LogsvcClient(ctx *fwcontext.RunContext) *log.SubLogger {
	log := r.Config.C.Clients.Log.WithFields(log.FieldMap{"hostname": r.Config.C.Hostname})

	if ctx.QueueItem != nil {
		return log.WithFields(logsvc.FieldMap{
			"run_id":     fmt.Sprintf("%v", ctx.QueueItem.Run.ID),
			"task_id":    fmt.Sprintf("%v", ctx.QueueItem.Run.Task.ID),
			"parent":     ctx.QueueItem.Run.Task.Submission.BaseRef.Repository.Name,
			"repository": ctx.QueueItem.Run.Task.Submission.HeadRef.Repository.Name,
			"sha":        ctx.QueueItem.Run.Task.Submission.HeadRef.SHA,
		})
	}

	return log
}

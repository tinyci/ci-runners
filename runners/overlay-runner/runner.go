package runner

import (
	"fmt"
	"os"
	"sync"

	"github.com/docker/docker/client"
	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/utils"
	"github.com/tinyci/ci-runners/fw"
	fwConfig "github.com/tinyci/ci-runners/fw/config"
	fwcontext "github.com/tinyci/ci-runners/fw/context"
	"github.com/tinyci/ci-runners/runners/overlay-runner/config"
)

// Runner encapsulates an infinite lifecycle overlay-runner.
type Runner struct {
	Config  *config.Config
	Docker  *client.Client
	running bool
	sync.Mutex
}

// Ready indicates the runner is ready.
func (r *Runner) Ready() bool {
	r.Lock()
	defer r.Unlock()
	return !r.running
}

// MakeRun makes a new run for the framework to use.
func (r *Runner) MakeRun(name string, runCtx *fwcontext.RunContext) (fw.Run, error) {
	r.Lock()
	defer r.Unlock()
	r.running = true

	return &Run{
		runner: r,
		name:   name,
		runCtx: runCtx,
	}, nil
}

// AfterRun sets the running state to false.
func (r *Runner) AfterRun(name string, runCtx *fwcontext.RunContext) {
	r.Lock()
	defer r.Unlock()
	r.running = false
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
		return eErr
	}

	if r.Config.C.Hostname == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return utils.WrapError(err, "Could not retrieve hostname")
		}
		r.Config.C.Hostname = hostname
	}

	r.Config.C.Clients.Log = r.Config.C.Clients.Log.WithFields(log.FieldMap{"hostname": r.Config.C.Hostname})

	return nil
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
	logger := r.Config.C.Clients.Log.WithFields(log.FieldMap{"hostname": r.Config.C.Hostname})

	if ctx.QueueItem != nil {
		return logger.WithFields(log.FieldMap{
			"run_id":     fmt.Sprintf("%v", ctx.QueueItem.Run.Id),
			"task_id":    fmt.Sprintf("%v", ctx.QueueItem.Run.Task.Id),
			"parent":     ctx.QueueItem.Run.Task.Submission.BaseRef.Repository.Name,
			"repository": ctx.QueueItem.Run.Task.Submission.HeadRef.Repository.Name,
			"sha":        ctx.QueueItem.Run.Task.Submission.HeadRef.Sha,
			"privileged": fmt.Sprintf("%v", ctx.QueueItem.Run.Settings.Privileged),
		})
	}

	return logger
}

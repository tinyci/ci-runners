package runner

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/errors"
	"github.com/tinyci/ci-runners/fw"
	"github.com/tinyci/ci-runners/fw/config"
)

// Runner encapsulates an infinite lifecycle overlay-runner.
type Runner struct {
	Config    *config.Config
	NextState bool
}

// Init is the bootstrap of the runner.
func (r *Runner) Init(ctx *fw.Context) *errors.Error {
	rand.Seed(time.Now().UnixNano())
	// we reload the clients on each run
	r.Config = &config.Config{Clients: &config.Clients{}}
	err := config.Load(ctx.CLIContext.GlobalString("config"), r.Config)
	if err != nil {
		return err
	}

	if r.Config.Hostname == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return errors.New(err).Wrap("Could not retrieve hostname")
		}
		r.Config.Hostname = hostname
	}

	r.Config.Clients.Log = r.Config.Clients.Log.WithFields(log.FieldMap{"queue": r.Config.QueueName, "hostname": r.Config.Hostname})
	return nil
}

// BeforeRun is executed before the next run is started.
func (r *Runner) BeforeRun(ctx *fw.Context) *errors.Error {
	r.NextState = rand.Intn(2) == 0
	r.LogsvcClient(ctx).Infof(ctx.RunCtx, "Run Commencing: Rolling the dice yielded %v - %v", r.NextState)

	return nil
}

// Run runs the CI job.
func (r *Runner) Run(ctx *fw.Context) (bool, *errors.Error) {
	return r.NextState, nil
}

// Hostname is the reported hostname of the machine; an identifier. Not
// necessary for anything and insecure, just ornamental.
func (r *Runner) Hostname() string {
	return r.Config.Hostname
}

// QueueName is the name of the queue this runner should be processing.
func (r *Runner) QueueName() string {
	return r.Config.QueueName
}

// QueueClient returns the queue client
func (r *Runner) QueueClient() *queue.Client {
	return r.Config.Clients.Queue
}

// LogsvcClient returns the system log client. Must be called after configuration is initialized
func (r *Runner) LogsvcClient(ctx *fw.Context) *log.SubLogger {
	wf := r.Config.Clients.Log.WithFields(log.FieldMap{"queue": r.Config.QueueName, "hostname": r.Config.Hostname})

	if ctx.QueueItem != nil {
		return wf.WithFields(log.FieldMap{
			"run_id":     fmt.Sprintf("%v", ctx.QueueItem.Run.ID),
			"task_id":    fmt.Sprintf("%v", ctx.QueueItem.Run.Task.ID),
			"parent":     ctx.QueueItem.Run.Task.Parent.Name,
			"repository": ctx.QueueItem.Run.Task.Ref.Repository.Name,
			"sha":        ctx.QueueItem.Run.Task.Ref.SHA,
		})
	}

	return wf
}

package runner

import (
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/utils"
	"github.com/tinyci/ci-runners/fw"
	"github.com/tinyci/ci-runners/fw/config"
	fwcontext "github.com/tinyci/ci-runners/fw/context"
)

// Runner encapsulates an infinite lifecycle overlay-runner.
type Runner struct {
	sync.Mutex
	Config    *config.Config
	NextState bool
}

// Run is a single run
type Run struct {
	runner *Runner
	name   string
	runCtx *fwcontext.RunContext
}

// Name is the name of the run
func (r *Run) Name() string {
	return r.name
}

func (r *Run) String() string {
	return r.Name()
}

// RunContext returns the context for this run
func (r *Run) RunContext() *fwcontext.RunContext {
	return r.runCtx
}

// Ready indicates the null runner is ready
func (r *Runner) Ready() bool {
	return true
}

// MakeRun makes a new run for the framework to use.
func (r *Runner) MakeRun(name string, runCtx *fwcontext.RunContext) (fw.Run, error) {
	return &Run{
		runner: r,
		name:   name,
		runCtx: runCtx,
	}, nil
}

// AfterRun does nothing in this runner.
func (r *Runner) AfterRun(string, *fwcontext.RunContext) {}

// Init is the bootstrap of the runner.
func (r *Runner) Init(ctx *fwcontext.Context) error {
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
			return utils.WrapError(err, "Could not retrieve hostname")
		}
		r.Config.Hostname = hostname
	}

	r.Config.Clients.Log = r.Config.Clients.Log.WithFields(log.FieldMap{"queue": r.Config.QueueName, "hostname": r.Config.Hostname})
	return nil
}

// BeforeRun is executed before the next run is started.
func (r *Run) BeforeRun() error {
	r.runner.Lock()
	defer r.runner.Unlock()
	r.runner.NextState = rand.Intn(2) == 0
	r.runner.LogsvcClient(r.runCtx).Infof(r.runCtx.Ctx, "Run Commencing: Rolling the dice yielded %v", r.runner.NextState)

	return nil
}

// Run runs the CI job.
func (r *Run) Run() (bool, error) {
	r.runner.Lock()
	defer r.runner.Unlock()
	return r.runner.NextState, nil
}

// AfterRun does nothing in the null-runner.
func (r *Run) AfterRun() error { return nil }

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
func (r *Runner) LogsvcClient(ctx *fwcontext.RunContext) *log.SubLogger {
	wf := r.Config.Clients.Log.WithFields(log.FieldMap{"queue": r.Config.QueueName, "hostname": r.Config.Hostname})

	if ctx.QueueItem != nil {
		return wf.WithFields(log.FieldMap{
			"run_id":     fmt.Sprintf("%v", ctx.QueueItem.Run.ID),
			"task_id":    fmt.Sprintf("%v", ctx.QueueItem.Run.Task.ID),
			"parent":     ctx.QueueItem.Run.Task.Submission.BaseRef.Repository.Name,
			"repository": ctx.QueueItem.Run.Task.Submission.HeadRef.Repository.Name,
			"sha":        ctx.QueueItem.Run.Task.Submission.HeadRef.SHA,
		})
	}

	return wf
}

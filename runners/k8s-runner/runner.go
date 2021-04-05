package runner

import (
	"fmt"
	"os"
	"sync"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/utils"
	"github.com/tinyci/ci-runners/fw"
	fwConfig "github.com/tinyci/ci-runners/fw/config"
	fwcontext "github.com/tinyci/ci-runners/fw/context"
	v1 "github.com/tinyci/k8s-api/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var v1Scheme *runtime.Scheme

func init() {
	v1Scheme = runtime.NewScheme()
	v1.AddToScheme(v1Scheme)
	corev1.AddToScheme(v1Scheme)
}

// Runner encapsulates an infinite lifecycle overlay-runner.
type Runner struct {
	Config *Config

	runCount uint
	sync.Mutex
}

// Ready returns true if the runner is ready to accept more work.
func (r *Runner) Ready() bool {
	r.Lock()
	defer r.Unlock()

	return r.runCount < r.Config.MaxConcurrency
}

// Init is the bootstrap of the runner.
func (r *Runner) Init(ctx *fwcontext.Context) error {
	// we reload the clients on each run
	r.Config = &Config{C: fwConfig.Config{Clients: &fwConfig.Clients{}}}
	err := fwConfig.Load(ctx.CLIContext.GlobalString("config"), r.Config)
	if err != nil {
		return err
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

// MakeRun makes a new run with a new context and name.
func (r *Runner) MakeRun(name string, runCtx *fwcontext.RunContext) (fw.Run, error) {
	r.Lock()
	defer r.Unlock()
	r.runCount++

	return &Run{
		name:   name,
		runCtx: runCtx,
		ctx:    runCtx.Ctx,
		logger: r.LogsvcClient(runCtx),
		runner: r,
	}, nil
}

// AfterRun decrements the run count
func (r *Runner) AfterRun(name string, runCtx *fwcontext.RunContext) {
	r.Lock()
	defer r.Unlock()
	r.runCount--
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
			"run_id":     fmt.Sprintf("%v", ctx.QueueItem.Run.ID),
			"task_id":    fmt.Sprintf("%v", ctx.QueueItem.Run.Task.ID),
			"parent":     ctx.QueueItem.Run.Task.Submission.BaseRef.Repository.Name,
			"repository": ctx.QueueItem.Run.Task.Submission.HeadRef.Repository.Name,
			"sha":        ctx.QueueItem.Run.Task.Submission.HeadRef.SHA,
		})
	}

	return logger
}

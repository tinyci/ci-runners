package runner

import (
	"context"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/tinyci/ci-agents/utils"
	fwcontext "github.com/tinyci/ci-runners/fw/context"
)

// Run is a single run.
type Run struct {
	runner *Runner
	runCtx *fwcontext.RunContext
	name   string

	containerID string
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

// BeforeRun is executed before the next run is started.
func (r *Run) BeforeRun() error {
	return nil
}

// Run runs the CI job.
func (r *Run) Run() (bool, error) {
	return r.RunDocker()
}

// AfterRun is for after the run cleanup
func (r *Run) AfterRun() error {
	// FIXME this fails sometimes, we'll classify the errors later. So much for "force".
	r.runner.Docker.ContainerRemove(context.Background(), r.containerID, types.ContainerRemoveOptions{Force: true})

	return nil
}

// StartCancelFunc launches a goroutine which waits for the cancel signal.
// Terminates when the run ends; one way or another. This function does not
// block.
func (r *Run) StartCancelFunc() {
	go func() {
		for {
			select {
			case <-r.runCtx.Ctx.Done():
				return
			default:
			}

			state, err := r.runner.Config.C.Clients.Queue.GetCancel(r.runCtx.Ctx, r.runCtx.QueueItem.Run.ID)
			if err != nil || !state {
				time.Sleep(time.Second)
				continue
			}

			r.runCtx.CancelFunc()
			return
		}
	}()
}

// StartLogger starts a goroutine that writes data produced on the reader to
// the log.
func (r *Run) StartLogger(rc io.Reader) {
	go func() {
		if err := r.runner.Config.C.Clients.Asset.Write(r.runCtx.Ctx, r.runCtx.QueueItem.Run.ID, rc); err != nil {
			r.runner.LogsvcClient(r.runCtx).Error(r.runCtx.Ctx, utils.WrapError(err, "Writing log for Run ID %d", r.runCtx.QueueItem.Run.ID))
		}
	}()
}

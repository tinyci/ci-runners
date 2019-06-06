package runner

import (
	"context"
	"io"
	"time"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/model"
	"github.com/tinyci/ci-runners/runners/overlay-runner/config"
)

// Run is the encapsulation of a CI run.
type Run struct {
	Logger      *log.SubLogger
	QueueItem   *model.QueueItem
	Config      *config.Config
	ContainerID string
	Status      bool
	Context     context.Context
	Cancel      context.CancelFunc
}

// NewRun constructs a new *Run.
func NewRun(context context.Context, cancelFunc context.CancelFunc, qi *model.QueueItem, c *config.Config, logger *log.SubLogger) *Run {
	if logger == nil {
		logger = c.C.Clients.Log
	}

	return &Run{
		QueueItem: qi,
		Config:    c,
		Context:   context,
		Cancel:    cancelFunc,
		Logger:    logger,
	}
}

// StartCancelFunc launches a goroutine which waits for the cancel signal.
// Terminates when the run ends; one way or another. This function does not
// block.
func (r *Run) StartCancelFunc() {
	go func() {
		for {
			select {
			case <-r.Context.Done():
				return
			default:
			}

			state, err := r.Config.C.Clients.Queue.GetCancel(r.Context, r.QueueItem.Run.ID)
			if err != nil || !state {
				time.Sleep(time.Second)
				continue
			}

			r.Cancel()
			return
		}
	}()
}

// StartLogger starts a goroutine that writes data produced on the reader to
// the log.
func (r *Run) StartLogger(rc io.Reader) {
	go func() {
		if err := r.Config.C.Clients.Asset.Write(r.Context, r.QueueItem.Run.ID, rc); err != nil {
			r.Logger.Error(r.Context, err.Wrapf("Writing log for Run ID %d", r.QueueItem.Run.ID))
		}
	}()
}

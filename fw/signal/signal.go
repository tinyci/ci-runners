// Package signal manages functionality surrounding signals from linux.
package signal

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/tinyci/ci-agents/clients/queue"
)

// Context is the context in which the handlers run under; they will be used to
// store clients as well as data about the run.
//
// Creating a new one of these for each run is a necessity.
type Context struct {
	// RunID of the current run
	RunID int64
	// QueueClient is a client to the queuesvc
	QueueClient *queue.Client
	// CancelFunc is the cancellation func for the global context for your runner's run.
	CancelFunc context.CancelFunc
	// CancelSignal and RunnerSignal are signal handlers set up by signal.Notify
	// in a normal fashion and are bound to unix signals. Cancel would typically
	// be bound to SIGINT while SIGTERM would be bound to the other.
	CancelSignal, RunnerSignal chan os.Signal
	// Done when closed will terminate the goroutines bound to the context.
	Done chan struct{}
}

// HandleCancel allows the user to program the queuesvc with a cancellation
// when a signal is received, and simultaneously trigger a cancellation of the
// run.
//
// To use this function, populate the *Context as described; then call this in a
// goroutine. Once triggered, it is assuming the daemon is shutting down and
// will trigger all cancellation behavior in the context.
func (ctx *Context) HandleCancel(waitTime time.Duration) {
	select {
	case <-ctx.Done:
		return
	case <-ctx.CancelSignal:
		ctx.CancelFunc()
	retry:
		canceled, err := ctx.QueueClient.GetCancel(ctx.RunID)
		if err != nil {
			fmt.Printf("Could not poll queuesvc; retrying in a second: %v\n", err)
			time.Sleep(time.Second)
			goto retry
		}

		if !canceled {
			if err := ctx.QueueClient.SetCancel(ctx.RunID); err != nil {
				fmt.Printf("Cannot cancel current job, retrying in 1s: %v\n", err)
				time.Sleep(time.Second)
				goto retry
			}
		}

		fmt.Printf("Signal received; will wait %v for cleanup to occur\n", waitTime)
		time.Sleep(waitTime)
		close(ctx.RunnerSignal)
	}
}

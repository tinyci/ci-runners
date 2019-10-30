package context

import (
	"context"
	"time"

	"github.com/tinyci/ci-agents/model"
	"github.com/urfave/cli"
)

// Context is the set of data generated from the runner framework controller
// code, but formatted to be sent to implementing hooks of the Runner
// interface.
//
// Please note that not all situations will call for all data to be
// represented, so check your struct values (or the docs) before using things.
type Context struct {
	// CLIContext is the urfave/cli.Context for managing CLI flags and other
	// functionality.
	CLIContext *cli.Context
}

// RunContext is specific to the run functions in fw; supplying additional data.
type RunContext struct {
	*Context

	// QueueItem is the item of the upcoming run; used in BeforeNextRun() and Run()
	QueueItem *model.QueueItem
	// RunStart is the time the run started. Populated only for Run().
	Start time.Time
	// RunCtx is the context.Context for the run; if closed the run should be canceled.
	Ctx context.Context
	// RunCancelFunc is the cancel func to close the above context.
	CancelFunc context.CancelFunc
}

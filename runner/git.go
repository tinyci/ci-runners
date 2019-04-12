package runner

import (
	"encoding/json"
	"io"

	"github.com/tinyci/ci-runners/git"
	"golang.org/x/oauth2"
)

func jsonIO(from, to interface{}) error {
	content, err := json.Marshal(from)
	if err != nil {
		return err
	}

	return json.Unmarshal(content, to)
}

// PullRepo retrieves the repository and puts it in the right spot.
func (r *Run) PullRepo(log io.Writer) (*git.RepoManager, error) {
	queueTok := r.QueueItem.Run.Task.Parent.Owners[0].Token
	tok := &oauth2.Token{}

	if err := jsonIO(queueTok, tok); err != nil {
		return nil, err
	}

	rm := &git.RepoManager{
		Log:          log,
		AccessToken:  tok.AccessToken,
		BaseRepoPath: r.Config.Runner.BaseRepoPath,
	}

	if err := rm.Init(r.Config, r.QueueItem.Run.Task.Parent.Name, r.QueueItem.Run.Task.Ref.Repository.Name); err != nil {
		return nil, err
	}

	if err := rm.CloneOrFetch(); err != nil {
		return nil, err
	}

	if err := rm.AddOrFetchFork(); err != nil {
		return nil, err
	}

	if err := rm.Checkout(r.QueueItem.Run.Task.Ref.SHA); err != nil {
		return nil, err
	}

	return rm, rm.Merge("origin/master")
}

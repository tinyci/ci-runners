package runner

import (
	"encoding/json"
	"io"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/types"
	"github.com/tinyci/ci-runners/fw/git"
)

func jsonIO(from, to interface{}) error {
	content, err := json.Marshal(from)
	if err != nil {
		return err
	}

	return json.Unmarshal(content, to)
}

// PullRepo retrieves the repository and puts it in the right spot.
func (r *Run) PullRepo(w io.Writer) (*git.RepoManager, error) {
	queueTok := r.QueueItem.Run.Task.Parent.Owner.Token
	tok := &types.OAuthToken{}

	if err := jsonIO(queueTok, tok); err != nil {
		return nil, err
	}

	rm := &git.RepoManager{
		Log:          w,
		AccessToken:  tok.Token,
		BaseRepoPath: r.Config.Runner.BaseRepoPath,
	}

	wf := r.Logger.WithFields(log.FieldMap{
		"owner":          r.QueueItem.Run.Task.Parent.Owner.Username,
		"base_repo_path": r.Config.Runner.BaseRepoPath,
		"repo_name":      r.QueueItem.Run.Task.Parent.Name,
	})

	if err := rm.Init(r.Config.Runner, wf, r.QueueItem.Run.Task.Parent.Name, r.QueueItem.Run.Task.Ref.Repository.Name); err != nil {
		wf.Errorf("Error initializing repo: %v", err)
		return nil, err
	}

	if err := rm.CloneOrFetch(); err != nil {
		wf.Errorf("Error cloning repo: %v", err)
		return nil, err
	}

	if err := rm.AddOrFetchFork(); err != nil {
		wf.Errorf("Error cloning fork: %v", err)
		return nil, err
	}

	if err := rm.Checkout(r.QueueItem.Run.Task.Ref.SHA); err != nil {
		wf.Errorf("Error checking out %v: %v", r.QueueItem.Run.Task.Ref.SHA, err)
		return nil, err
	}

	if err := rm.Merge("origin/master"); err != nil {
		wf.Errorf("Error merging master for %v: %v", r.QueueItem.Run.Task.Ref.SHA, err)
		return nil, err
	}

	return rm, nil
}

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
	queueTok := r.QueueItem.Run.Task.Submission.BaseRef.Repository.Owner.Token
	tok := &types.OAuthToken{}

	if err := jsonIO(queueTok, tok); err != nil {
		return nil, err
	}

	rm := &git.RepoManager{
		Config:      r.Config.Runner,
		Log:         w,
		AccessToken: tok.Token,
	}

	wf := r.Logger.WithFields(log.FieldMap{
		"owner":          r.QueueItem.Run.Task.Submission.BaseRef.Repository.Owner.Username,
		"base_repo_path": r.Config.Runner.BaseRepoPath,
		"repo_name":      r.QueueItem.Run.Task.Submission.BaseRef.Repository.Name,
	})

	if err := rm.Init(r.Config.Runner, wf, r.QueueItem.Run.Task.Submission.BaseRef.Repository.Name, r.QueueItem.Run.Task.Submission.HeadRef.Repository.Name); err != nil {
		wf.Errorf(r.Context, "Error initializing repo: %v", err)
		return nil, err
	}

	if err := rm.CloneOrFetch(r.Context); err != nil {
		wf.Errorf(r.Context, "Error cloning repo: %v", err)
		return nil, err
	}

	if err := rm.AddOrFetchFork(); err != nil {
		wf.Errorf(r.Context, "Error cloning fork: %v", err)
		return nil, err
	}

	if err := rm.Checkout(r.QueueItem.Run.Task.Submission.HeadRef.SHA); err != nil {
		wf.Errorf(r.Context, "Error checking out %v: %v", r.QueueItem.Run.Task.Submission.HeadRef.SHA, err)
		return nil, err
	}

	if err := rm.Merge("origin/master"); err != nil {
		wf.Errorf(r.Context, "Error merging master for %v: %v", r.QueueItem.Run.Task.Submission.HeadRef.SHA, err)
		return nil, err
	}

	return rm, nil
}

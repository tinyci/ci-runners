package runner

import (
	"encoding/json"
	"io"
	"path"
	"strings"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/errors"
	"github.com/tinyci/ci-agents/types"
	"github.com/tinyci/ci-runners/fw/git"
)

func jsonIO(from, to interface{}) *errors.Error {
	content, err := json.Marshal(from)
	if err != nil {
		return errors.New(err)
	}

	return errors.New(json.Unmarshal(content, to))
}

// PullRepo retrieves the repository and puts it in the right spot.
func (r *Run) PullRepo(w io.Writer) (*git.RepoManager, *errors.Error) {
	queueTok := r.runCtx.QueueItem.Run.Task.Submission.BaseRef.Repository.Owner.Token
	tok := &types.OAuthToken{}

	if err := jsonIO(queueTok, tok); err != nil {
		return nil, err
	}

	rm := &git.RepoManager{
		Config:      r.runner.Config.Runner,
		Log:         w,
		AccessToken: tok.Token,
	}

	defaultBranchName := strings.TrimLeft(strings.TrimLeft(r.runCtx.QueueItem.Run.Task.Submission.BaseRef.RefName, "heads/"), "tags/")

	wf := r.runner.LogsvcClient(r.runCtx).WithFields(log.FieldMap{
		"owner":          r.runCtx.QueueItem.Run.Task.Submission.BaseRef.Repository.Owner.Username,
		"base_repo_path": r.runner.Config.Runner.BaseRepoPath,
		"repo_name":      r.runCtx.QueueItem.Run.Task.Submission.BaseRef.Repository.Name,
	})

	if err := rm.Init(r.runner.Config.Runner, wf, r.runCtx.QueueItem.Run.Task.Submission.BaseRef.Repository.Name, r.runCtx.QueueItem.Run.Task.Submission.HeadRef.Repository.Name); err != nil {
		wf.Errorf(r.runCtx.Ctx, "Error initializing repo: %v", err)
		return nil, err
	}

	if err := rm.CloneOrFetch(r.runCtx.Ctx, defaultBranchName); err != nil {
		wf.Errorf(r.runCtx.Ctx, "Error cloning repo: %v", err)
		return nil, err
	}

	if err := rm.AddOrFetchFork(); err != nil {
		wf.Errorf(r.runCtx.Ctx, "Error cloning fork: %v", err)
		return nil, err
	}

	if err := rm.Checkout(r.runCtx.QueueItem.Run.Task.Submission.HeadRef.SHA); err != nil {
		wf.Errorf(r.runCtx.Ctx, "Error checking out %v: %v", r.runCtx.QueueItem.Run.Task.Submission.HeadRef.SHA, err)
		return nil, err
	}

	if err := rm.Merge(path.Join("origin", defaultBranchName)); err != nil {
		wf.Errorf(r.runCtx.Ctx, "Error merging master for %v: %v", r.runCtx.QueueItem.Run.Task.Submission.HeadRef.SHA, err)
		return nil, err
	}

	return rm, nil
}

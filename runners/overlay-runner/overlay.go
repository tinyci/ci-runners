package runner

import (
	"io/ioutil"

	"github.com/tinyci/ci-runners/fw/git"
	"github.com/tinyci/ci-runners/fw/overlay"
)

// MountRepo mounts the repo through overlayfs so we can quickly clean up the
// build artifacts and other work done in the container.
func (r *Run) MountRepo(gr *git.RepoManager) (*overlay.Mount, error) {
	work, err := ioutil.TempDir("", "")
	if err != nil {
		return nil, err
	}

	upper, err := ioutil.TempDir("", "")
	if err != nil {
		return nil, err
	}

	target, err := ioutil.TempDir("", "")
	if err != nil {
		return nil, err
	}

	m := &overlay.Mount{
		Lower:  gr.RepoPath,
		Work:   work,
		Upper:  upper,
		Target: target,
	}

	return m, m.Mount()
}

// MountCleanup cleans up the mount and any dirs created.
func (r *Run) MountCleanup(m *overlay.Mount) error {
	if err := m.Unmount(); err != nil {
		return err
	}

	return m.Cleanup()
}

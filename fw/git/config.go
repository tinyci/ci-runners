package git

import (
	"path/filepath"

	"github.com/tinyci/ci-agents/errors"
)

const (
	defaultLoginScriptPath = "/tmp/tinyci-github-login.sh"
	defaultBaseRepoPath    = "/tmp/git"
	defaultGitUserName     = "tinyCI runner"
	defaultGitEmail        = "no-reply@example.org"
)

// Config manages various one-off tidbits about the runner's git paths and
// other data. You must manually merge this with your runner's configuration if
// you wish to use the runner framework, see fw/config documentation for more
// information.
type Config struct {
	LoginScriptPath string `yaml:"login_script_path"`
	BaseRepoPath    string `yaml:"base_repo_path"`
}

// Validate corrects or errors out when the configuration doesn't match
// expectations.
func (rc *Config) Validate() error {
	if rc.LoginScriptPath == "" {
		rc.LoginScriptPath = defaultLoginScriptPath
	}

	if !filepath.IsAbs(rc.LoginScriptPath) {
		return errors.New("login_script_path must be absolute")
	}

	if rc.BaseRepoPath == "" {
		rc.BaseRepoPath = defaultBaseRepoPath
	}

	if !filepath.IsAbs(rc.BaseRepoPath) {
		return errors.New("base_repo_path must be absolute")
	}

	return nil
}

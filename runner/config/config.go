package config

import (
	"errors"
	"os"
	"path"
	"path/filepath"

	"github.com/tinyci/ci-agents/clients/asset"
	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/config"
)

const (
	defaultLoginScriptPath = "/tmp/tinyci-github-login.sh"
	defaultBaseRepoPath    = "/tmp/git"
)

// Config is the on-disk runner configuration
type Config struct {
	Hostname     string       `yaml:"hostname"`
	QueueName    string       `yaml:"queue"`
	ClientConfig ClientConfig `yaml:"clients"`
	Runner       RunnerConfig `yaml:"runner"`

	Clients *Clients `yaml:"-"`
}

// RunnerConfig manages various one-off tidbits about the runner's paths and other data.
type RunnerConfig struct {
	LoginScriptPath string `yaml:"login_script_path"`
	BaseRepoPath    string `yaml:"base_repo_path"`
}

// Validate corrects or errors out when the configuration doesn't match expectations.
func (rc *RunnerConfig) Validate() error {
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

// ClientConfig is the configuration settings for each service we need a client to.
type ClientConfig struct {
	TLS   config.CertConfig `yaml:"tls"`
	Asset string            `yaml:"assetsvc"`
	Queue string            `yaml:"queuesvc"`
	Log   string            `yaml:"logsvc"`
}

// Clients contains the actual clients.
type Clients struct {
	Log   *log.SubLogger
	Queue *queue.Client
	Asset *asset.Client
}

// Load loads the runner configuration
func Load(filename string) (*Config, error) {
	c := &Config{Clients: &Clients{}}
	if err := config.Parse(filename, c); err != nil {
		return nil, err
	}

	cert, err := c.ClientConfig.TLS.Load()
	if err != nil {
		return nil, err
	}

	if c.ClientConfig.Log != "" {
		log.ConfigureRemote(c.ClientConfig.Log, cert)
	}

	c.Clients.Log = log.NewWithData(path.Base(os.Args[0]), log.FieldMap{"queue": c.QueueName})

	c.Clients.Queue, err = queue.New(c.ClientConfig.Queue, cert)
	if err != nil {
		return nil, err
	}

	c.Clients.Asset, err = asset.NewClient(cert, c.ClientConfig.Asset)
	if err != nil {
		return nil, err
	}

	return c, nil
}

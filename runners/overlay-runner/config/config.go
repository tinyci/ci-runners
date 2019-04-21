package config

import (
	"os"
	"path"

	"github.com/tinyci/ci-agents/clients/asset"
	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/config"
	"github.com/tinyci/ci-runners/git"
)

// Config is the on-disk runner configuration
type Config struct {
	Hostname     string       `yaml:"hostname"`
	QueueName    string       `yaml:"queue"`
	ClientConfig ClientConfig `yaml:"clients"`
	Runner       git.Config   `yaml:"git"`

	Clients *Clients `yaml:"-"`
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

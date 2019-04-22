package config

import (
	"os"
	"path"

	"github.com/tinyci/ci-agents/clients/asset"
	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/config"
)

// Configurator is a loose wrapper around configuration objects. The
// configuration is capable of return a Config struct from this package -- but
// that may be an inner or wrapped component.
//
// A second call is provided to allow additional configuration beyond what has
// been imagined here.
type Configurator interface {
	Config() *Config
	ExtraLoad() error
}

// Config is the on-disk runner configuration
type Config struct {
	Hostname     string       `yaml:"hostname"`
	QueueName    string       `yaml:"queue"`
	ClientConfig ClientConfig `yaml:"clients"`

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

// Config satisfies the configurator interface.
func (c *Config) Config() *Config {
	return c
}

// ExtraLoad does nothing for the basic configuration.
func (c *Config) ExtraLoad() error {
	return nil
}

// Load loads the runner configuration
func Load(filename string, c Configurator) error {
	if err := config.Parse(filename, c); err != nil {
		return err
	}

	cfg := c.Config()

	cert, err := cfg.ClientConfig.TLS.Load()
	if err != nil {
		return err
	}

	if cfg.ClientConfig.Log != "" {
		log.ConfigureRemote(cfg.ClientConfig.Log, cert)
	}

	cfg.Clients.Log = log.NewWithData(path.Base(os.Args[0]), log.FieldMap{"queue": cfg.QueueName, "hostname": cfg.Hostname})

	cfg.Clients.Queue, err = queue.New(cfg.ClientConfig.Queue, cert)
	if err != nil {
		return err
	}

	cfg.Clients.Asset, err = asset.NewClient(cert, cfg.ClientConfig.Asset)
	if err != nil {
		return err
	}

	return nil
}

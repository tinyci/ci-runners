// Package config presents a standard framework-compatible configuration for runners.
//
// While not intending to complete your configuration, it should make things
// easier. Wrapping this package in another, with the `inline` property for
// gopkg.in/yaml.v2 set for the struct holding this config is a good way to
// achieve that.
//
// Example:
//
//		package config
//
//		import (
//			fwConfig "github.com/tinyci/ci-runners/fw/config"
//			"github.com/tinyci/ci-runners/fw/git"
//		)
//
//		// Config is the on-disk runner configuration
//		type Config struct {
//			C      fwConfig.Config `yaml:"c,inline"`
//			Runner git.Config      `yaml:"git"`
//		}
//
//		// Config returns the configuration as a basic framework config so fw/config.Load() can work appropriately.
//		func (c *Config) Config() *fwConfig.Config {
//			return &c.C
//		}
//
//		// ExtraLoad does nothing and satisfies the fw/config.Config interface
//		func (c *Config) ExtraLoad() error {
//			return nil
//		}
//
package config

import (
	"os"
	"path"

	"github.com/tinyci/ci-agents/clients/asset"
	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/clients/queue"
	"github.com/tinyci/ci-agents/config"
	"github.com/tinyci/ci-agents/errors"
)

// Configurator is a loose wrapper around configuration objects. The
// configuration is capable of return a Config struct from this package -- but
// that may be an inner or wrapped component.
//
// A second call is provided to allow additional configuration beyond what has
// been imagined here.
//
// People leveraging the runner framework with configurations (just about
// everyone) must implement this interface.
type Configurator interface {
	// Config is the call that returns the configuration used from this package.
	Config() *Config
	// ExtraLoad is for doing any additional work that the framework does not
	// prescribe already.
	ExtraLoad() *errors.Error
}

// Config is the on-disk runner configuration
type Config struct {
	// Hostname is the identifier for the runner -- defaults to the machine hostname.
	Hostname string `yaml:"hostname"`
	// QueueName is the name of the queue the runner should listen on.
	QueueName string `yaml:"queue"`
	// ClientConfig is the configuration of the various clients runners typically use.
	ClientConfig ClientConfig `yaml:"clients"`

	// Clients is a locally-populated struct (see Load()) based on ClientConfig.
	// It contains the actual client structs.
	Clients *Clients `yaml:"-"`
}

// ClientConfig is the configuration settings for each service we need a client
// to. Please note that these are not urls -- just host:port pairs.
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
func (c *Config) ExtraLoad() *errors.Error {
	return nil
}

// Load loads the runner configuration and configures clients -- logsvc,
// queuesvc, and assetsvc clients with optional TLS settings.
func Load(filename string, c Configurator) *errors.Error {
	if err := config.Parse(filename, c); err != nil {
		return errors.New(err)
	}

	cfg := c.Config()

	cert, err := cfg.ClientConfig.TLS.Load()
	if err != nil {
		return err
	}

	if cfg.ClientConfig.Log != "" {
		log.ConfigureRemote(cfg.ClientConfig.Log, cert, false)
	}

	cfg.Clients.Log = log.NewWithData(path.Base(os.Args[0]), log.FieldMap{"queue": cfg.QueueName, "hostname": cfg.Hostname})

	cfg.Clients.Queue, err = queue.New(cfg.ClientConfig.Queue, cert, false)
	if err != nil {
		return err
	}

	cfg.Clients.Asset, err = asset.NewClient(cfg.ClientConfig.Asset, cert, false)
	if err != nil {
		return err
	}

	return nil
}

package config

import (
	fwConfig "github.com/tinyci/ci-runners/fw/config"
	"github.com/tinyci/ci-runners/fw/git"
)

// Config is the on-disk runner configuration
type Config struct {
	C      fwConfig.Config `yaml:"c,inline"`
	Runner git.Config      `yaml:"git"`
}

// Config returns the configuration as a basic framework config so fw/config.Load() can work appropriatey.
func (c *Config) Config() *fwConfig.Config {
	return &c.C
}

// ExtraLoad does nothing and satisfies the fw/config.Config interface
func (c *Config) ExtraLoad() error {
	return nil
}

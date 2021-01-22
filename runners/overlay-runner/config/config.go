package config

import (
	"github.com/tinyci/ci-agents/errors"
	"github.com/tinyci/ci-runners/fw/config"
	"github.com/tinyci/ci-runners/fw/git"
)

// Config is the on-disk runner configuration
type Config struct {
	C              config.Config `yaml:"c,inline"`
	Runner         git.Config    `yaml:"git"`
	OverlayTempdir string        `yaml:"overlay_tempdir"`
}

// Config returns the configuration as a basic framework config so fw/config.Load() can work appropriately.
func (c *Config) Config() *config.Config {
	return &c.C
}

// ExtraLoad does nothing and satisfies the fw/config.Config interface
func (c *Config) ExtraLoad() *errors.Error {
	return nil
}

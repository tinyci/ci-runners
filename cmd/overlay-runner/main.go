package main

import (
	"time"

	"github.com/tinyci/ci-runners/fw"
	"github.com/tinyci/ci-runners/fw/utils"
	runner "github.com/tinyci/ci-runners/runners/overlay-runner"
)

func main() {
	err := fw.Run(fw.Entrypoint{
		Usage: "Run tinyci jobs with overlayfs and docker",
		Description: `
This runner provides a docker interface to running tinyci builds. It also
leverages an overlayfs backend and git cache to make clones fast.
`,
		Launch:          &runner.Runner{},
		TeardownTimeout: 10 * time.Second,
	})
	if err != nil {
		utils.ErrOut(err)
	}
}

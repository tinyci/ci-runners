package main

import (
	"github.com/tinyci/ci-runners/fw"
	"github.com/tinyci/ci-runners/fw/utils"
	runner "github.com/tinyci/ci-runners/runners/null-runner"
)

func main() {
	err := fw.Launch(&fw.Entrypoint{
		Usage: "Run tinyci jobs with overlayfs and docker",
		Description: `
This runner mocks a real runner and provides no function but to report statuses.
`,
		Launch:          &runner.Runner{},
		TeardownTimeout: 0,
	})
	if err != nil {
		utils.ErrOut(err)
	}
}

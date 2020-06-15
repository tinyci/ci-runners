package main

import (
	"time"

	"github.com/tinyci/ci-runners/fw"
	"github.com/tinyci/ci-runners/fw/utils"
	runner "github.com/tinyci/ci-runners/runners/k8s-runner"
)

func main() {
	err := fw.Launch(&fw.Entrypoint{
		Usage: "Run tinyci jobs with kubernetes",
		Description: `
This runner runs both internal and external to kubernetes, and provides a
mechanism for running work through the CIJob controller.
`,
		Launch:          &runner.Runner{},
		TeardownTimeout: 10 * time.Second,
	})
	if err != nil {
		utils.ErrOut(err)
	}
}

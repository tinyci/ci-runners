module github.com/tinyci/ci-runners

go 1.12

replace github.com/docker/docker v1.13.1 => github.com/docker/engine v0.0.0-20190620014054-c513a4c6c298

require (
	github.com/Microsoft/go-winio v0.4.12 // indirect
	github.com/creack/pty v1.1.7
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/docker v1.13.1
	github.com/docker/engine v1.13.1 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/fatih/color v1.7.0
	github.com/opencontainers/go-digest v1.0.0-rc1 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/pkg/errors v0.8.1
	github.com/tinyci/ci-agents v0.1.0
	github.com/urfave/cli v1.20.0
	golang.org/x/sys v0.0.0-20190626221950-04f50cda93cb
)

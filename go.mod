module github.com/tinyci/ci-runners

go 1.12

replace github.com/docker/docker => github.com/docker/engine v1.4.2-0.20190822205725-ed20165a37b4

require (
	github.com/Microsoft/go-winio v0.4.14 // indirect
	github.com/creack/pty v1.1.7
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/docker v0.0.0-00010101000000-000000000000
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/fatih/color v1.7.0
	github.com/opencontainers/go-digest v1.0.0-rc1 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/pkg/errors v0.8.1
	github.com/tinyci/ci-agents v0.1.2-0.20190907091021-c253cae0559e
	github.com/urfave/cli v1.21.0
	golang.org/x/sys v0.0.0-20190904154756-749cb33beabd
)

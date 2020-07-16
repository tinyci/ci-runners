module github.com/tinyci/ci-runners

go 1.14

require (
	github.com/creack/pty v1.1.11
	github.com/docker/docker v1.13.1
	github.com/fatih/color v1.9.0
	github.com/fluxcd/source-controller v0.0.4 // indirect
	github.com/go-openapi/errors v0.19.6
	github.com/mattn/go-sqlite3 v2.0.1+incompatible // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pkg/errors v0.9.1
	github.com/tinyci/ci-agents v0.2.7
	github.com/tinyci/k8s-api v0.0.0-20200713103720-2891f14f1429
	github.com/urfave/cli v1.22.4
	golang.org/x/net v0.0.0-20200707034311-ab3426394381 // indirect
	golang.org/x/sys v0.0.0-20200625212154-ddb9806d33ae
	google.golang.org/genproto v0.0.0-20200707001353-8e8330bf89df // indirect
	k8s.io/api v0.18.5
	k8s.io/apimachinery v0.18.5
	k8s.io/client-go v0.18.5
	sigs.k8s.io/controller-runtime v0.6.0
)

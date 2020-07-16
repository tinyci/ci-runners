GO_VERSION ?= 1.14

all:
	go install -v ./...

checks:
	go get github.com/golangci/golangci-lint/...
	golangci-lint run -v 

staticcheck:
	go get -d ./...
	go get honnef.co/go/tools/...
	staticcheck ./...

update-modules:
	rm -f go.mod go.sum
	GO111MODULE=on go get -u -d ./...
	GO111MODULE=on go mod tidy

dist:
	rm -rf build
	mkdir -p build
	docker pull golang:${GO_VERSION}
	docker run --rm \
		-e GO111MODULE=on \
		-e GOBIN=/tmp/bin \
		-e GOCACHE=/tmp/.cache \
		-u $$(id -u):$$(id -g) \
		-v ${PWD}/build:/tmp/bin \
		-w /go/src/github.com/tinyci/ci-runners \
		-v ${PWD}:/go/src/github.com/tinyci/ci-runners \
		golang:${GO_VERSION} \
		go install -v ./...
	tar cvzf release.tar.gz build/*

dist-image: dist
	box -t tinyci/runners:latest box-builds/box-dist.rb

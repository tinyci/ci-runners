all: checks
	GO111MODULE=on go install -v ./...

checks:
	bash checks.sh

staticcheck:
	go get -d ./...
	go get honnef.co/go/tools/...
	staticcheck ./...

update-modules:
	rm -f go.mod go.sum
	GO111MODULE=on go get -u -d ./...
	GO111MODULE=on go mod tidy

docker-build:
	docker build -t ci-runners \
		--build-arg GITHUB_TOKEN=${GITHUB_TOKEN} \
		--force-rm .

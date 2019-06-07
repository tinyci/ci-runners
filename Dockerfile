FROM golang:1.12
ARG GITHUB_TOKEN
RUN mkdir -p /go/src/github.com/tinyci
COPY . /go/src/github.com/tinyci/ci-runners
WORKDIR /go/src/github.com/tinyci/ci-runners
ENV GO111MODULE=on
RUN make all

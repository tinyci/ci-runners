FROM golang:1.12.1
ARG GITHUB_TOKEN
RUN mkdir -p /go/src/github.com/tinyci
COPY . /go/src/github.com/tinyci/ci-runners
WORKDIR /go/src/github.com/tinyci/ci-runners
ENV GO111MODULE=on
RUN echo "#!/bin/sh \\n\
echo $GITHUB_TOKEN" > /root/githelper.sh \
 && chmod +x /root/githelper.sh \
 && export GIT_ASKPASS=/root/githelper.sh \
 && GO111MODULE=on go install -v ./... \
 && rm /root/githelper.sh
RUN make all

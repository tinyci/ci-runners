#!/bin/sh

/usr/local/bin/docker-entrypoint.sh dockerd &

exec "$@"

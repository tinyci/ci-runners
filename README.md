## Runners for tiny CI: Execute runs in a variety of ways.

A runner process is simply something that listens to the tinyCI queuesvc,
allowing it to get the next available job, and report the status of that job.

That's pretty much it; this simplicity allows us to do literally anything
between the time that the run has begun (queue shift) and the time the status
of the finished run has been reported.

Our current runner implementations are:

## Overlay Runner (overlay-runner)

This is a bare-bones runner that utilizes docker and overlayfs to achieve a
performant and secure way to run isolated unit tests.

Git clones are kept permanently on the system (until a cache threshold is
reached; then they are wiped). Each CI run incorporates an overlayfs-powered
"air gap" between the git repository and the container running the run. The
container can write all it wants to the git repository's directory, but the
overlayfs will capture that. At the end of the run, the overlayfs is removed
from disk, returning the repository to a pristine state.

## Framework

We have a runner framework to make it easy to build runners; please see our
[GoDoc](https://godoc.org/github.com/tinyci/ci-runners/fw) for more information
on how to use it!

## Authors

- [Erik Hollensbe](https://github.com/erikh) -- Overlay Runner

## License

Mozilla Public License 2.0: https://www.mozilla.org/en-US/MPL/2.0/

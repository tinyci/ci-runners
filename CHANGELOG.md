## 0.2.7 -- Thu 16 Jul 2020 04:53:58 AM UTC

- The runner framework now handles concurrency
- k8s runner now in experimental availability
- tinyci/runners image now being created

## 0.2.1 -- Wed Nov 05 22:49:56 PDT 2019

- The runner framework would leak goroutines and channels during signal handling.

## 0.2.0 -- Mon Nov 04 09:33:27 PDT 2019

- Runner framework `fw` reworked, easier to use and more reliable in a number of scenarios
- Graceful restart support! Send a SIGHUP to terminate the runner after its current run completes.
- Fix situations where logging would cause a lockup of the runner if it could not reach the logsvc.
- Up-to-date on golang 1.13

## 0.1.1 -- Sat Jul 13 10:24:48 PDT 2019

Fix some fd leaks in the overlay-runner.

## 0.1.0 -- Wed Jul 3 11:13:35 PDT 2019

Initial release!

# Linko

This is a toy URL shortener project, to be used as the starter repo for the Logging and Telemetry course on [Boot.dev](https://www.boot.dev/).

It's intentionally small, a little messy, and realistic enough to practice adding logs, metrics, and traces in Go.

## Build

Build linko with the following command (to include git sha and build time in
logs):

```sh
go build -ldflags "-X my/package/build.GitSHA=$(git rev-parse HEAD) -X my/package/build.BuildTime=$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
```

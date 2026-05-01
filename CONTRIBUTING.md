# Contributing

Build the dashboard first: `cd dashboard && npm run build`.

Then build the Go binary from the repo root: `go build ./...`.

Run the test suite after that: `go test ./...`.

The order matters and must be followed exactly: dashboard build first, then Go build, then Go tests, because the Go binary embeds the built dashboard assets.

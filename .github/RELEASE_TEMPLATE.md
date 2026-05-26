# Release Template

## Summary

- Version: `vX.Y.Z`
- Date: `YYYY-MM-DD`
- Commit: `<sha>`

## Highlights

- 

## Security and hardening

- 

## Migration notes

- 

## Checks

- [ ] `go vet ./...`
- [ ] `govulncheck ./...`
- [ ] `go test ./...`
- [ ] `go test -tags integration ./...`
- [ ] `go build ./...`

## Artifacts

- [ ] Linux amd64
- [ ] Linux arm64
- [ ] macOS amd64
- [ ] macOS arm64
- [ ] Windows amd64
- [ ] Docker `ghcr.io/td-02/mcpscope:vX.Y.Z`
- [ ] Docker `ghcr.io/td-02/mcpscope:latest`
- [ ] Homebrew formula updated

# CI Integration

This guide shows how to use `mcpscope snapshot` and `mcpscope diff` in GitHub Actions so pull requests can detect MCP schema changes before they merge.

## Baseline workflow

1. Capture a known-good schema snapshot and commit it to the repository as `baseline.json`.
2. In CI, run `mcpscope snapshot` against the current server build to produce `current.json`.
3. Run `mcpscope diff baseline.json current.json --exit-code --format json`.
4. Fail the workflow if breaking changes are detected.
5. Post the JSON diff as a PR comment for reviewer context.

## Example commands

```bash
./mcpscope snapshot --server ./path/to/mcp-server --output current.json
./mcpscope diff baseline.json current.json --exit-code --format json
```

## Storing the baseline

- Keep `baseline.json` in the repository near the MCP server code, or under `schemas/`.
- Treat the baseline as versioned API surface.
- Update it only when schema changes are intentional and reviewed.

## Updating the baseline

When a schema change is expected:

1. Run `mcpscope snapshot` locally against the updated server.
2. Review the `mcpscope diff` output carefully.
3. Replace `baseline.json` with the new snapshot.
4. Include the schema diff in the pull request description.

## Pull request checks

The example workflow in [examples/github-actions/mcp-schema-check.yml](../examples/github-actions/mcp-schema-check.yml) demonstrates:

- building `mcpscope`
- capturing `current.json`
- diffing against `baseline.json`
- commenting on the PR with the diff
- blocking the merge on breaking changes

# Contributing

## Setup

Prerequisite: Go ≥ 1.26 (no C toolchain needed — the SQLite driver is pure Go).

```bash
git clone https://github.com/JOSETRA44/mef-mcp
cd mef-mcp
go build ./...
go test ./...
```

`go install ./cmd/...` puts `mef` and `mef-mcp` in `$(go env GOPATH)/bin`.

## Tests

- `go test ./...` is **hermetic**: no network, no real cache or database. Every
  upstream is replayed from fixtures in each module's `testdata/`, captured
  from the live services.
- `go vet ./...` and `gofmt -l .` must be clean.
- When an upstream changes shape, capture a fresh fixture, commit it next to
  the parser change, and record what changed in `docs/SOURCES.md`.
- **Fixtures must look like the real thing, not like the ideal.** A fixture
  that says `MINISTERIO DE SALUD` passed while the live service — which writes
  `M. DE SALUD` — returned nothing. If a service is inconsistent, the fixture
  carries both forms.
- `internal/mcpserver` asserts every tool description carries purpose, usage
  guidance and behaviour (DOCS-STANDARD §6), and `internal/meta` asserts the
  README tool table matches the registry. Both fail if you add a tool and stop
  there.

## Layout

| Package | What lives there |
|---|---|
| `internal/core` | The command contract: params, input validation, sinks, errors and exit codes |
| `internal/httpx`, `cache` | One HTTP client with the disk cache, retries and per-host politeness |
| `internal/render` | The five output formats and the JSON envelope |
| `internal/analysis` | Statistics and findings (totals, ranking, concentration, idle budget) |
| `internal/report` | Deliverables: xlsx, self-contained HTML, Markdown |
| `internal/modules/*` | One MEF source each, plus `informes` (analysis/reports) and `asistente` (routing) |

Commands that need another command's data run it through a capturing sink
(see `internal/modules/informes`) instead of re-querying: a report of spending
is the output of `mef_gasto`, not a second implementation of it.

## The contract

This project is part of a workspace governed by two documents:

- **ARSENAL-SPEC** — output formats, the JSON envelope, exit codes, disk cache,
  `describe --json`, `--all`/`--max`. Tests in `internal/render`,
  `internal/cli` and `internal/cache` enforce it.
- **DOCS-STANDARD** — LICENSE, `server.json`, the README order and tool
  description quality. `internal/meta` tests keep versions and names in sync.

## Adding a data source

Read [`docs/ADDING-A-MODULE.md`](docs/ADDING-A-MODULE.md). In short: one
package under `internal/modules/<name>` implementing `core.Module`, one line in
`internal/modules/all.go`, fixtures and tests. CLI, MCP tools and the manifest
need no changes.

## Pull requests

- One logical change per PR, with tests.
- Tool descriptions are code: say what it returns, when to use it instead of a
  sibling, how it behaves (latency, caching, empty results) and what it does
  not do.
- Update `CHANGELOG.md` under an `Unreleased` heading.

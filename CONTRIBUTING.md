# Contributing

Thank you for your interest in contributing to `deadcode`.

## Getting Started

1. Fork the repository and clone your fork.
2. Install Go 1.25.0 or later.
3. Run `go build ./...` to verify everything compiles.
4. Run `go vet ./...` to check for common issues.

## Development

The project has no external runtime dependencies beyond the Go standard library
and `golang.org/x/tools`. All analysis logic lives under `internal/`.

### Key packages

| Package              | Role |
|----------------------|------|
| `internal/analyzer`  | SSA construction and RTA-based reachability analysis |
| `internal/fixer`     | AST-based safe removal of dead declarations |
| `internal/report`    | Output formatting (text, JSON, patch) |
| `internal/config`    | CLI flag parsing |
| `internal/logger`    | Colored terminal output and spinner |
| `internal/color`     | ANSI helpers |

### Testing with the sample module

The `testdata/sample/` directory contains a small module you can run the tool
against from the project root:

```bash
go run . --entry=./testdata/sample/cmd ./testdata/sample/...
```

Expected: `unusedHelper`, `unusedConst`, `unusedVar`, `UnusedType`, and
`UnusedExported` are reported. `keptByAnnotation` is skipped due to the
`//deadcode:keep` annotation.

## Submitting a Pull Request

- Keep pull requests focused on a single concern.
- Add or update tests in `testdata/` when changing analysis logic.
- Run `go vet ./...` and `go build ./...` before opening a PR.
- Write clear commit messages that explain *why* the change is needed.

## Reporting Issues

Open an issue on GitHub with:

- The Go version (`go version`).
- The command you ran and the full output.
- A minimal reproduction case if possible.

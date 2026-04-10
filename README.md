# deadcode

<div align="center">

**A CLI tool that detects and optionally removes dead code across a Go project or monorepo.**

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue)](https://golang.org/dl/)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-fynx/deadcode.svg)](https://pkg.go.dev/github.com/go-fynx/deadcode)
[![Go Report Card](https://goreportcard.com/badge/github.com/go-fynx/deadcode)](https://goreportcard.com/report/github.com/go-fynx/deadcode)


It uses **SSA (Static Single Assignment)** and **RTA (Rapid Type Analysis)** to build a precise call graph, then reports every function, variable, constant, and type that is unreachable from any `main` entry point.

</div>


## Features

- Whole-program analysis using `go/ssa` and `golang.org/x/tools/go/callgraph/rta`
- Detects dead functions, methods, package-level vars, consts, and types
- Two confidence levels: **HIGH** (unexported + unreachable) and **MEDIUM** (exported but unused internally)
- Interface-aware: types used only via interface dispatch are not misclassified as dead
- `init()` is always considered reachable — never flagged
- `//deadcode:keep` annotation to protect intentional dead-looking code
- Interactive file selection when applying fixes
- Safe AST-based deletion using `go/parser` + `go/format` — no string replacement
- Git dirty-tree warning before any fix is applied
- Optional `.bak` backups before modifying files
- JSON, text, and patch-style output formats
- Skips generated files (`// Code generated … DO NOT EDIT`) and test files by default
- `NO_COLOR` / `FORCE_COLOR` environment variable support

---

## Install

```bash
go install github.com/go-fynx/deadcode@latest
```

Or build from source:

```bash
git clone https://github.com/go-fynx/deadcode.git
cd deadcode
go build -o deadcode .
```

---

## Quick Start

```bash
# Scan all packages in the current module
deadcode ./...

# Specify where main packages live (recommended for monorepos)
deadcode --entry=./cmd ./...

# Preview what would be removed — no files are modified
deadcode --fix-dry-run ./...

# Apply removals interactively (prompts you to pick files)
deadcode --fix ./...
```

---

## Usage

```
deadcode [flags] [packages]
```

`packages` accepts any pattern valid for `go build` (e.g. `./...`, `./internal/...`, `github.com/example/myapp/...`). Defaults to `./...` when omitted.

### Examples

```bash
# Basic scan
deadcode ./...

# Monorepo: discover main packages only under ./cmd
deadcode --entry=./cmd ./...

# Show only high-confidence dead code (unexported + unreachable)
deadcode --confidence=high ./...

# Include exported-but-unused symbols (medium confidence) as well
deadcode --confidence=all ./...

# JSON output (useful for CI pipelines or tooling)
deadcode --json ./...

# Patch-style reference listing
deadcode --output-patch ./...

# Dry run — show what would be removed
deadcode --fix-dry-run ./...

# Remove dead code interactively with backups
deadcode --fix --backup ./...

# Remove dead code from one specific file
deadcode --fix --fix-file=internal/helper/helper.go ./...

# Ignore generated mocks and wire output
deadcode --ignore="mock_*,wire_gen.go" ./...

# Verbose: print loaded packages and RTA roots
deadcode --verbose ./...
```

---

## Flags

| Flag               | Default  | Description |
|--------------------|----------|-------------|
| `--entry`          | `""`     | Directory containing `main` packages. When empty, all loaded `main` packages are used as RTA roots. |
| `--confidence`     | `all`    | Confidence filter: `high` (unexported only) or `all` / `medium` (includes exported). |
| `--json`           | `false`  | Emit results as JSON. |
| `--output-patch`   | `false`  | Emit a patch-style reference listing. |
| `--ignore`         | `""`     | Comma-separated glob patterns for files or packages to skip (e.g. `mock_*,vendor/*`). |
| `--fix`            | `false`  | Remove dead code. Prompts for file selection unless `--fix-file` is set. |
| `--fix-dry-run`    | `false`  | Preview removals without writing any file. Mutually exclusive with `--fix`. |
| `--fix-file`       | `""`     | Limit fix to a single file path (relative or absolute). |
| `--backup`         | `false`  | Write `.bak` files before modifying or deleting. |
| `--skip-tests`     | `true`   | Exclude `*_test.go` files from analysis. |
| `--skip-generated` | `true`   | Exclude files with a `// Code generated … DO NOT EDIT` header. |
| `--verbose`        | `false`  | Print loaded package names and RTA root selection details. |

---

## Confidence Levels

| Level      | Criteria                                            | Safe to auto-remove? |
|------------|-----------------------------------------------------|----------------------|
| **HIGH**   | Unexported symbol, not reachable from any entry point | Yes — `--fix` removes these by default |
| **MEDIUM** | Exported symbol, not referenced within the analyzed packages | Review manually — may be part of a public API |

> **Tip:** Start with `--confidence=high` for zero-risk cleanup. Add `--confidence=all` only when you are certain the package is not imported externally (e.g. an internal monorepo service).

---

## Keep Annotation

Add `//deadcode:keep` anywhere in a function's doc comment to exclude it from analysis:

```go
// ProcessLegacy handles the old V1 request format.
//
//deadcode:keep — retained for rollback compatibility until Q3
func ProcessLegacy(req *Request) error {
    ...
}
```

The substring `deadcode:keep` is matched anywhere in the doc comment block.

---

## Confidence Semantics When Fixing

`--fix` respects `--confidence`:

| `--confidence` | What gets removed |
|----------------|-------------------|
| `high`         | Only unexported unreachable symbols |
| `medium` / `all` | Both HIGH and MEDIUM symbols |

---

## Output Formats

### Text (default)

```
deadcode — dead code detector for Go
──────────────────────────────────────────────────
  ✓ Loaded 5 package(s)
  ✓ SSA built successfully
  ✓ RTA complete — 38 reachable function(s)
  ✓ Scan complete — 4 dead item(s) found

Dead code detected:
────────────────────────────────────────────────────────────────────────────────
 HIGH   internal/helper/helper.go:29 → func unusedHelper
 HIGH   internal/helper/helper.go:7  → const unusedConst
 MEDIUM internal/helper/helper.go:34 → func UnusedExported
 MEDIUM internal/helper/helper.go:9  → const UsedConst
────────────────────────────────────────────────────────────────────────────────

Summary:
  Total functions:     8
  Reachable:           4
  Dead functions:      2
  Dead consts:         2
  Skipped files:       0
```

### JSON (`--json`)

```json
{
  "dead_code": [
    {
      "name": "unusedHelper",
      "kind": "func",
      "file": "internal/helper/helper.go",
      "line": 29,
      "package": "example.com/sample/internal/helper",
      "confidence": "HIGH",
      "is_exported": false
    }
  ],
  "summary": {
    "total_functions": 8,
    "reachable_functions": 4,
    "dead_functions": 2,
    "dead_vars": 0,
    "dead_consts": 2,
    "dead_types": 1,
    "skipped_files": 0,
    "dead_files": []
  }
}
```

### Patch reference (`--output-patch`)

```
# Dead code locations (patch-style reference)
# Use --fix or --fix-dry-run to generate actual file modifications

--- a/internal/helper/helper.go
+++ b/internal/helper/helper.go
@@ -29 [HIGH] func unusedHelper @@
-// dead code: remove func unusedHelper
```

---

## Interactive Fix Mode

When `--fix` is used without `--fix-file`, an interactive prompt lets you select which files to clean up:

```
Files with dead code to fix:
────────────────────────────────────────────────────
  [1]  internal/helper/helper.go  (2 dead item(s))
        − func unusedHelper (line 29)
        − const unusedConst (line 7)
  [2]  pkg/api/handler.go  (1 dead item(s))
        − func legacyHandler (line 45)
────────────────────────────────────────────────────

→ Enter file numbers to fix (e.g. 1,3,5), or all for all:
```

Enter a comma-separated list of numbers or `all`. Press Enter without input to abort without changes.

---

## Safety Guarantees

- **Report-only by default** — no file is modified unless `--fix` or `--fix-dry-run` is passed.
- **AST-based removal** — uses `go/parser` and `go/format`; never does string replacement.
- **Git dirty-tree warning** — warns if the working tree has uncommitted changes before applying fixes.
- **Backup support** — `--backup` creates `.bak` files before any modification or deletion.
- **Generated files excluded** — files with `// Code generated … DO NOT EDIT` are skipped by default.
- **Test files excluded** — `*_test.go` files are skipped by default.
- **`init()` always reachable** — package init functions are never reported as dead.
- **Interface-aware** — types whose methods are reachable via interface dispatch are never misclassified.

---

## Project Structure

```
deadcode/
├── main.go                  CLI entry point
├── internal/
│   ├── analyzer/            SSA + RTA whole-program dead code analysis
│   ├── color/               ANSI color helpers (NO_COLOR / FORCE_COLOR aware)
│   ├── config/              CLI flag parsing and validation
│   ├── fixer/               AST-based safe deletion with interactive file selection
│   ├── logger/              Structured CLI output with spinner and color
│   └── report/              Output formatting (text, JSON, patch)
└── testdata/
    └── sample/              Minimal sample module for manual testing
```

---

## Limitations

- Requires a complete, buildable Go module (all imports must resolve).
- RTA is a **sound over-approximation**: it may consider some functions reachable that are not called at runtime (e.g. functions registered in a plugin registry that is never triggered). This means false negatives (missed dead code) but never false positives.
- Does not analyze code reachable only via `reflect` or `unsafe` pointer manipulation.
- Exported symbols used by external packages outside the analyzed set are reported as MEDIUM confidence — always review before removing.

---

## Environment Variables

| Variable      | Effect |
|---------------|--------|
| `NO_COLOR`    | Disable all ANSI color output |
| `FORCE_COLOR` | Force ANSI color even when stdout is not a TTY |

---

## License

MIT — see [LICENSE](LICENSE).

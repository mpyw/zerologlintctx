# Repository instructions

Guidance for contributors and coding agents working in this repository.

## Project Overview

**zerologlintctx** is a Go linter that enforces context propagation in [zerolog](https://pkg.go.dev/github.com/rs/zerolog) logging chains. It detects:

1. Event chains missing `.Ctx(ctx)` when context is available
2. Direct logging calls (`Print`, `Printf`, `Println`, `Write`) on a logger that does not carry the context

| Package | What it does |
|---|---|
| `internal/zerologapi` | What each zerolog function and method does, and what zerolog does with memory |
| `internal/ssaflow` | Where a value in SSA form comes from; knows nothing of zerolog |
| `internal/chain` | The tracer and the checker, on top of the two above |
| `internal/directive` | `//zerologlintctx:ignore` comments |

See [Architecture](./docs/ARCHITECTURE.md) for internal design details.

## Development Commands

```bash
# Run ALL tests (ALWAYS use this before committing)
./test_all.sh

# Run tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Build CLI
go build -o bin/zerologlintctx ./cmd/zerologlintctx

# Run linter on itself
go vet -vettool=./bin/zerologlintctx ./...

# Run golangci-lint
golangci-lint run ./...

# Diff the diagnostics on real modules against testdata/corpus/
scripts/corpus.sh
```

> [!IMPORTANT]
> Always use `./test_all.sh` before committing. This runs all tests including linting.

## Releasing

Releases are cut from GitHub Actions, never locally:

- **Tag and Release** (`tag_and_release.yml`) — the normal path: creates the `vX.Y.Z` tag, then builds and publishes.
- `tag.yml` / `release.yml` are the two halves, dispatchable on their own when a tag already exists.

`release.yml` runs goreleaser (`.goreleaser.yaml`) once for all six targets — darwin/linux/windows x amd64/arm64, `CGO_ENABLED=0` — and uploads the archives plus `checksums.txt` to the release. Those assets are what makes `mise use -g "github:mpyw/zerologlintctx"` work, so a release with no assets breaks the documented install path. goreleaser is pinned to 2.17.1: 2.18.0 attests every build artifact, which fails the job.

Validate config changes with `goreleaser check`, and dry-run with `goreleaser release --snapshot --clean --skip=publish`.

## Testing Strategy

- Use `analysistest` for all analyzer tests
- Test fixtures use `// want` comments for expected diagnostics
- Test structure:
  - `===== SHOULD REPORT =====` - Cases that should trigger warnings
  - `===== SHOULD NOT REPORT =====` - Negative cases
  - `===== EDGE CASES =====` - Corner cases

### Corpus Golden Test

`scripts/corpus.sh` builds the analyzer, runs it over pinned commits of real modules that use zerolog heavily, and diffs the result against `testdata/corpus/<name>.txt`.

> [!IMPORTANT]
> A refactor that claims no change in behavior must keep the corpus diff empty, as well as `./test_all.sh`.

| Command | What it does |
|---|---|
| `scripts/corpus.sh` | Check every module; exits 1 and prints a unified diff on a change |
| `scripts/corpus.sh -update` | Rewrite the golden files; commit them with an intended change |
| `scripts/corpus.sh reva topaz` | Only the named modules |

- The modules and their commits are pinned in the `corpus` list at the top of the script: autobrr, cloudflared, fleet-server, reva, topaz.
- A golden line is `relative/file.go:line:col: message`, sorted and deduplicated. Paths are relative to the module root.
- Only diagnostics are compared. Packages that fail to load are skipped by the analyzer, and their errors are dropped.
- The analysis runs with `GOOS=linux GOARCH=amd64 CGO_ENABLED=0`, so every host analyzes the same files. Only the root module of each repository is analyzed.
- Clones are cached in `${XDG_CACHE_HOME:-~/.cache}/zerologlintctx-corpus` (override with `ZEROLOGLINTCTX_CORPUS_DIR`). The first run downloads the modules and their dependencies; a run with a warm cache takes about 1.5 minutes.
- CI runs it in `corpus.yml` on pull requests that change Go code, `go.mod`, `go.sum` or the corpus files, and on manual dispatch.
- Requires git, go and jq. Go must be at least the highest `go` directive in the corpus (1.27.3).

### Testdata Organization

```
testdata/src/zerolog/      # One package per topic, each its own subtest of TestZerolog
├── alias/       # zerolog/context types reached through aliases and defined types
├── api/         # Event API: levels, fields, terminators, MsgFunc, Enabled, Discard
├── basic/       # Simple good/bad cases, global logger
├── closures/    # Closures, nested func literals, goroutines, captured variables
├── ctxname/     # Which context is in scope: several, named _, shadowed, pointer, type parameter
├── deferred/    # Deferred terminators and deferred closures
├── directive/   # Ignore directives and directive problems
├── directlog/   # Print, Printf, Println, Write on a logger
├── freshctx/    # context.Background() or TODO() in place of the context in scope
├── go127/       # Go 1.27 syntax (promoted field keys, generic methods)
├── iife/        # IIFE, func literals called through a variable, helpers
├── inplace/     # Event.Ctx with its result unused, Logger.UpdateContext
├── logger/      # Logger transformations: With().Logger(), Level, Output, New, Nop, Ctx
├── memory/      # Struct fields, embedded fields, pointers, arrays, slices, maps, channels
├── methods/     # Method values and method expressions in the chain
├── phi/         # Branches, switches, loops, goto, select, pointers picked on a branch
├── stores/      # Which stores to a variable in memory reach a read
└── variables/   # Local variables: reassignment, shadowing, interfaces, results
```

## Code Style

- Follow standard Go conventions
- Use `go/analysis` framework
- Prefer type-based checks over name-based (see Architecture)
- Unexported types by default; only export what's needed

### Comment Guidelines

**Comments should inform newcomers, not document history.**

- Bad: `// moved from evil.go`
- Bad: `// refactored in session 5`
- Good: `// LIMITATION(#115): a write through another reference is not seen`

## Key Design Decisions

1. **One description of the zerolog API**: `zerologapi.Classify` tells what each function and method does, from its signature and a small table of named exceptions. Code in `internal/chain` matches no zerolog names of its own
2. **Value flow apart from zerolog**: `internal/ssaflow` finds what a read from memory can see: places, stores, closures, walks of the control flow. It imports nothing of this module, and asks a `Model` (`zerologapi.Model`) for the two facts about zerolog it needs. Keep zerolog out of it
3. **SSA tracing**: One `tracer`, which traces each value in a state: its zerolog type, and the sources of context that still count for it. `Logger.Output` narrows the sources. Each trace answers `yes`, `no` or `neutral`, and a value reached again through a cycle is `neutral`: it adds no source
4. **Zero false positives**: Prefer missing issues over false alarms

## Known Limitations

- **Helper function returns**: Can't track through interprocedural analysis (IIFE is supported)
- **Channel send/receive**: Can't trace through channels
- **Closure-modified capture**: Closure writes to outer variable
- **Events made in a loop**: When a value can hold the event of the latest run of a call on one path, and of an earlier run on another, a `Ctx` call on it counts for the latest run on both paths
- **In-place `Ctx` through another reference**: `e.Ctx(ctx)` with its result unused is not seen by a terminator in a closure that captures `e`, or in a func literal `e` is passed to. In the same function, a `Ctx` call on any value that holds the same event counts: an earlier read of the same variable or field, or another variable the event was copied to, unless another event is stored in between
- **Events stored back**: When a read can see several events, a `Ctx` call on an earlier read that sees the same events counts for each of them, even where a path stores one of those events back over another in between
- **Panics**: A deferred call is checked at the returns after its `defer` statement, not at the points where a panic could run it
- **Slice and map elements**: An event read back from a slice or map is not traced, so one stored with `.Ctx(ctx)` is still reported
- **Elements at an index computed in a loop**: A store at the same index, on the same iteration, is not taken to write the element, so a store without `.Ctx(ctx)` on an earlier iteration, or the caller's value in a slice parameter, may still be what the read sees
- **Writes through another reference**: A write through a pointer passed to a function, another slice of the same array, or another pointer parameter is not seen, so the value stored before it is taken as the one read
- **Fresh contexts not seen**: A fresh context given to `Ctx` in place, with its result unused, does not replace the context the event had

These are documented in test cases with `// LIMITATION` comments, tagged `// LIMITATION(#n)` where an issue tracks the case.

All fixture packages import the same zerolog stub under `testdata/src/github.com/rs/zerolog`. A new directory under `testdata/src/zerolog/` is picked up by `TestZerolog` as a subtest, and by the plugin test through `zerolog/...`.

## Related Documentation

- [README.md](./README.md) - User documentation
- [Architecture](./docs/ARCHITECTURE.md) - Internal design and detection logic

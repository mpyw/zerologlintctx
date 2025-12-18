# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**zerologlintctx** is a Go linter that enforces context propagation in zerolog logging chains. It detects cases where a `context.Context` is available in function parameters but not properly passed to zerolog chains via `.Ctx(ctx)`.

### Supported Checker

- **zerolog**: Detect missing `.Ctx(ctx)` in zerolog chains using SSA-based analysis

### Directives

- `//zerologlintctx:ignore` - Suppress warnings for the next line or same line

## Architecture

```
zerologlintctx/
├── cmd/
│   └── zerologlintctx/         # CLI entry point (singlechecker)
│       └── main.go
├── internal/                    # SSA-based analysis (flat structure)
│   ├── analyzer.go              # Entry point, function context discovery
│   ├── tracing.go               # SSA value tracing logic
│   ├── types.go                 # Type utilities, tracer implementations
│   └── ignore.go                # Ignore directive handling
├── testdata/
│   └── src/
│       ├── zerolog/             # Test fixtures
│       └── github.com/rs/zerolog/  # Library stubs
├── analyzer.go                  # Public analyzer definition
├── analyzer_test.go             # Integration tests
└── README.md
```

### Key Design Decisions

1. **Type-safe analysis**: Uses `go/types` for accurate detection (not just name-based)
2. **SSA for zerolog**: Uses SSA form to track Event values through assignments
3. **Zero false positives**: Prefer missing issues over false alarms
4. **Strategy Pattern**: Uses Strategy Pattern for tracing Event/Logger/Context types
5. **Flat internal structure**: Single `internal/` package for simplicity (single checker)

### Zerolog SSA Strategy Pattern

The zerolog checker uses SSA analysis with Strategy Pattern for tracing:

```
┌─────────────┐     ┌─────────────┐     ┌───────────────┐
│ eventTracer │────▶│loggerTracer │────▶│ contextTracer │
│  (Event)    │◀────│  (Logger)   │◀────│   (Context)   │
└─────────────┘     └─────────────┘     └───────────────┘
        │                   │                    │
        └───────────────────┴────────────────────┘
                            │
                     ┌──────▼──────┐
                     │ traceCommon │  (Phi, UnOp, FreeVar, etc.)
                     └─────────────┘
```

- `ssaTracer` interface: `hasContext()`, `continueOnReceiverType()`
- Each tracer knows its context sources and delegates across type boundaries
- Handles: variable assignments, conditionals (Phi), closures, struct fields, defer

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
```

> [!IMPORTANT]
> Always use `./test_all.sh` before committing. This runs all tests including linting.

## Testing Strategy

- Use `analysistest` for all analyzer tests
- Test fixtures use `// want` comments for expected diagnostics
- Test structure:
  - `===== SHOULD REPORT =====` - Cases that should trigger warnings
  - `===== SHOULD NOT REPORT =====` - Negative cases
  - `===== EDGE CASES =====` - Corner cases

### Testdata Organization

```
testdata/src/zerolog/
├── basic.go           # Simple good/bad cases, ignore directives
├── evil.go            # General edge cases (nesting, closures, conditionals)
├── evil_ssa.go        # SSA-specific patterns (IIFE, Phi, channels)
├── evil_logger.go     # Logger transformation patterns
└── with_logger.go     # WithLogger-specific tests
```

## Code Style

- Follow standard Go conventions
- Use `go/analysis` framework
- Prefix file-specific variables with meaningful names
- Unexported types by default; only export what's needed

### Comment Guidelines

**Comments should inform newcomers, not document history.**

- ❌ Bad: `// moved from evil.go`
- ❌ Bad: `// refactored in session 5`
- ✓ Good: `// LIMITATION: cross-function tracking not supported`

## Known SSA Limitations

The zerolog checker has some known limitations due to SSA analysis constraints:

- **IIFE/Helper returns**: Can't track through interprocedural analysis
- **Channel send/receive**: Can't trace through channels
- **Method values**: `msg := e.Msg; msg("test")` - can't track method values

These are documented in test cases with `LIMITATION` comments.

## Related Projects

- [goroutinectx](https://github.com/mpyw/goroutinectx) - Goroutine context propagation linter (sibling project)
- [contextcheck](https://github.com/kkHAIKE/contextcheck) - Detects `context.Background()`/`context.TODO()` misuse

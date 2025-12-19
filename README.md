# zerologlintctx

[![Go Reference](https://pkg.go.dev/badge/github.com/mpyw/zerologlintctx.svg)](https://pkg.go.dev/github.com/mpyw/zerologlintctx)
[![Go Report Card](https://goreportcard.com/badge/github.com/mpyw/zerologlintctx)](https://goreportcard.com/report/github.com/mpyw/zerologlintctx)
[![Codecov](https://codecov.io/gh/mpyw/zerologlintctx/graph/badge.svg)](https://codecov.io/gh/mpyw/zerologlintctx)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

> [!NOTE]
> This project was written by AI (Claude Code).

A Go linter that checks zerolog logging chains for missing context propagation.

## Overview

`zerologlintctx` detects cases where a `context.Context` is available in function parameters but not properly passed to zerolog logging chains via `.Ctx(ctx)`.

## Installation & Usage

### Using [`go install`](https://pkg.go.dev/cmd/go#hdr-Compile_and_install_packages_and_dependencies)

```bash
go install github.com/mpyw/zerologlintctx/cmd/zerologlintctx@latest
zerologlintctx ./...
```

### Using [`go tool`](https://pkg.go.dev/cmd/go#hdr-Run_specified_go_tool) (Go 1.24+)

```bash
# Add to go.mod as a tool dependency
go get -tool github.com/mpyw/zerologlintctx/cmd/zerologlintctx@latest

# Run via go tool
go tool zerologlintctx ./...
```

### Using [`go run`](https://pkg.go.dev/cmd/go#hdr-Compile_and_run_Go_program)

```bash
go run github.com/mpyw/zerologlintctx/cmd/zerologlintctx@latest ./...
```

> [!CAUTION]
> To prevent supply chain attacks, pin to a specific version tag instead of `@latest` in CI/CD pipelines (e.g., `@v0.4.0`).

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-test` | `true` | Analyze test files (`*_test.go`) — built-in driver flag |

Generated files (containing `// Code generated ... DO NOT EDIT.`) are always excluded and cannot be opted in.

### Examples

```bash
# Exclude test files from analysis
zerologlintctx -test=false ./...
```

## What It Checks

### zerolog

Detects zerolog logging chains missing `.Ctx(ctx)`:

```go
func handler(ctx context.Context, log zerolog.Logger) {
    // Bad: missing .Ctx(ctx)
    log.Info().Str("key", "value").Msg("hello")

    // Good: includes .Ctx(ctx)
    log.Info().Ctx(ctx).Str("key", "value").Msg("hello")
}
```

The analyzer uses SSA (Static Single Assignment) form to track Event values through variable assignments, conditionals, and closures, ensuring accurate detection even in complex code patterns.

## Directives

### `//zerologlintctx:ignore`

Suppress warnings for a specific line:

```go
func handler(ctx context.Context, log zerolog.Logger) {
    //zerologlintctx:ignore - intentionally not passing context
    log.Info().Msg("background task")
}
```

The comment can be on the same line or the line above.

## Design Principles

1. **Zero false positives** - Prefer missing issues over false alarms
2. **Type-safe analysis** - Uses `go/types` for accurate detection
3. **SSA-based tracking** - Uses SSA form to track Event values through assignments and closures
4. **Nested function support** - Correctly tracks context through closures

## Related Tools

- [zerologlint](https://github.com/ykadowak/zerologlint) - General zerolog linting rules
- [contextcheck](https://github.com/kkHAIKE/contextcheck) - Detects `context.Background()`/`context.TODO()` usage and missing context parameters

`zerologlintctx` is complementary to these tools:
- `zerologlint` provides general zerolog best practices
- `contextcheck` warns about creating new contexts when one should be propagated
- `zerologlintctx` specifically warns about not using an available context in zerolog chains

## License

MIT

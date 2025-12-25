# Architecture

This document describes the internal architecture of zerologlintctx.

## Overview

zerologlintctx uses SSA (Static Single Assignment) form analysis to track zerolog chains and detect missing context propagation. The analyzer is built on the [`go/analysis`](https://pkg.go.dev/golang.org/x/tools/go/analysis) framework.

```
zerologlintctx/
├── cmd/zerologlintctx/        # CLI entry point (singlechecker)
├── internal/                  # Core analysis logic
│   ├── analyzer.go            # Entry point, function context discovery
│   ├── directive/             # Comment directive handling
│   │   └── ignore.go          # //zerologlintctx:ignore parsing
│   ├── ssa/                   # SSA-based analysis
│   │   ├── checker.go         # Checker struct, SSA inspection
│   │   └── tracing.go         # Value tracing and context checking
│   └── typeutil/              # Type checking utilities
│       └── zerolog.go         # Zerolog type predicates
├── testdata/src/              # Test fixtures and library stubs
├── analyzer.go                # Public analyzer definition
└── analyzer_test.go           # Integration tests
```

## Detection Logic

### What Gets Detected

| Pattern | Condition | Message |
|---------|-----------|---------|
| Event chain missing `.Ctx()` | `isEvent(recv) && returnsVoid(fn)` | `zerolog call chain missing .Ctx(ctx)` |
| Direct logging on Logger | `isLogger(recv) && returnsVoid(fn) && hasPrefix("Print")` | `zerolog direct logging bypasses context; use Event chain with .Ctx(ctx)` |
| Direct logging via log package | `zerologLogPath && returnsVoid(fn) && hasPrefix("Print")` | `zerolog direct logging bypasses context; use Event chain with .Ctx(ctx)` |

### Type-Based Analysis

The analyzer uses **return type checking** instead of method name hardcoding for most detection:

| Transition | Detection Method | Examples |
|------------|------------------|----------|
| Logger → Event | `returnsEvent(fn)` | `Info()`, `Debug()`, `Err()`, `WithLevel()` |
| Logger → Context | `returnsContext(fn)` | `With()` |
| Context → Logger | `returnsLogger(fn)` | `Logger()` |
| Event → Event | `shouldContinueOnReceiver` | `Str()`, `Int()`, `Ctx()`, etc. |
| Context → Context | `shouldContinueOnReceiver` | `Str()`, `Int()`, `Ctx()`, etc. |

This approach automatically handles new zerolog methods without code changes.

### Exception: Direct Logging

For Logger's direct logging methods, we use a **name prefix check**:

```go
// Detected (Print* prefix + void return)
logger.Print(...)
logger.Printf(...)
logger.Println(...)

// NOT detected (void but not Print*)
logger.UpdateContext(...)  // Configuration, not logging
```

This is necessary because `UpdateContext` also returns void but is not a logging method.

## SSA Tracing

The tracing system follows SSA values backwards to find if context was set.
All tracing logic is implemented in `internal/ssa/tracing.go`.

```
┌──────────────┐     ┌──────────────┐     ┌────────────────┐
│ tracerEvent  │────▶│ tracerLogger │────▶│ tracerContext  │
│   (Event)    │◀────│   (Logger)   │◀────│   (Context)    │
└──────────────┘     └──────────────┘     └────────────────┘
        │                    │                     │
        └────────────────────┴─────────────────────┘
                             │
                      ┌──────▼──────┐
                      │ traceCommon │
                      └─────────────┘
```

### Context Checking

Each tracer type knows its context sources:

**tracerEvent:**
- `Event.Ctx(ctx)` → Found
- `Context.Ctx(ctx)` → Found
- `zerolog.Ctx(ctx)` / `log.Ctx(ctx)` → Found
- `Logger.Info()` etc. → Delegate to tracerLogger
- `Context.Logger()` → Delegate to tracerContext

**tracerLogger:**
- `zerolog.Ctx(ctx)` / `log.Ctx(ctx)` → Found
- `Context.Logger()` → Delegate to tracerContext
- `Logger.With()` → Self-delegate (traces parent Logger)

**tracerContext:**
- `Context.Ctx(ctx)` → Found
- `Logger.With()` → Delegate to tracerLogger

### Common SSA Patterns

`traceCommon` in `internal/ssa/tracing.go` handles shared patterns:

- **Phi nodes** - Conditional assignments (all branches must have context)
- **UnOp** - Pointer dereferences
- **Alloc** - Local variable allocation (traces stored values)
- **FreeVar** - Closure captured variables
- **FieldAddr/Field** - Struct field access
- **Store tracking** - Values stored at addresses

## Terminator Detection

Event chain terminators are detected by:

1. Receiver is `*zerolog.Event`
2. Method returns void

```go
// All terminators (void methods on Event)
event.Msg("...")
event.Msgf("...", args)
event.MsgFunc(func() string { ... })
event.Send()
```

## Known Limitations

Due to SSA analysis constraints:

- **Helper function returns**: Can't track through interprocedural analysis (IIFE is supported)
- **Channel send/receive**: Can't trace through channels
- **Closure-modified capture**: Closure writes to outer variable

These are documented in test cases with `// LIMITATION` comments.

## Testing

Uses `analysistest` with `// want` comments:

```go
func bad(ctx context.Context, log zerolog.Logger) {
    log.Info().Msg("test") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func good(ctx context.Context, log zerolog.Logger) {
    log.Info().Ctx(ctx).Msg("test") // OK
}
```

### Test Organization

```
testdata/src/zerolog/
├── basic.go        # Simple cases, ignore directives
├── evil.go         # Edge cases (nesting, closures)
├── evil_ssa.go     # SSA-specific patterns (Phi, FreeVar)
├── evil_logger.go  # Logger patterns, direct logging
└── with_logger.go  # WithLogger-specific tests
```

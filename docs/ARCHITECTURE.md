# Architecture

This document describes the internal architecture of zerologlintctx.

## Overview

zerologlintctx uses SSA (Static Single Assignment) form analysis to track zerolog chains and detect missing context propagation. The analyzer is built on the [`go/analysis`](https://pkg.go.dev/golang.org/x/tools/go/analysis) framework.

```
zerologlintctx/
├── cmd/zerologlintctx/        # CLI entry point (singlechecker)
├── plugin/                    # golangci-lint module plugin
├── internal/                  # Core analysis logic
│   ├── chain/                 # SSA-based chain analysis
│   │   ├── checker.go         # Checker: checks each call, go and defer
│   │   ├── callee.go          # Resolves and classifies the function a call runs
│   │   ├── context.go         # Which functions have a context in scope
│   │   ├── tracing.go         # Tracer types, handoffs, value tracing
│   │   ├── phi.go             # Phi nodes and edges that lead back to them
│   │   ├── store.go           # Values stored at an address
│   │   ├── aggregate.go       # Paths into a struct or array, and its copies
│   │   ├── match.go           # Whether two addresses are the same
│   │   └── value.go           # SSA plumbing the other files stand on
│   ├── directive/             # Comment directive handling
│   │   └── ignore.go          # //zerologlintctx:ignore parsing, directive problems
│   └── typeutil/              # Type checking utilities
│       └── zerolog.go         # Zerolog type and signature predicates
├── testdata/src/              # Test fixtures and library stubs
├── analyzer.go                # Public analyzer, directive reporting
└── analyzer_test.go           # Integration tests
```

## Detection Logic

### What Gets Detected

| Pattern | Condition | Message |
|---------|-----------|---------|
| Event chain missing `.Ctx()` | `IsEvent(recv) && ReturnsVoid(sig)` | `zerolog call chain missing .Ctx(ctx)` |
| Direct logging on Logger | `IsLogger(recv) && ReturnsVoid(sig) && hasPrefix("Print")` | `zerolog direct logging bypasses context; use Event chain with .Ctx(ctx)` |
| Direct logging via log package | `IsZerologLogPkg(pkg) && ReturnsVoid(sig) && hasPrefix("Print")` | `zerolog direct logging bypasses context; use Event chain with .Ctx(ctx)` |

Every call is checked the same way, whether it is a plain call, a `go`
statement or a `defer` statement. `calleeOf` in `callee.go` resolves the
declared function a call runs, and its receiver, in each of the three forms a
method can take:

| Form | SSA | Receiver |
|------|-----|----------|
| `e.Msg("x")` | `(*Event).Msg(e, "x")` | `Args[0]` |
| `m := e.Msg; m("x")` | `(*Event).Msg$bound(e)`, then a call to it | `Bindings[0]` |
| `f := (*Event).Msg; f(e, "x")` | `(*Event).Msg$thunk(e, "x")` | `Args[0]` |

### Type-Based Analysis

The analyzer uses **return type checking** instead of method name hardcoding for most detection:

| Transition | Detection Method | Examples |
|------------|------------------|----------|
| Logger → Event | `ReturnsEvent(sig)` | `Info()`, `Debug()`, `Err()`, `WithLevel()` |
| Logger → Context | `ReturnsContext(sig)` | `With()` |
| Context → Logger | `ReturnsLogger(sig)` | `Logger()` |
| Event → Event | receiver is the traced type | `Str()`, `Int()`, `Ctx()`, etc. |
| Context → Context | receiver is the traced type | `Str()`, `Int()`, `Ctx()`, etc. |

This approach automatically handles new zerolog methods without code changes.

Type identity checks resolve aliases (`types.Unalias`) before comparing the
package path and type name, since go/types represents every alias declaration
as a `*types.Alias` node. Without that, a `type MyCtx = context.Context`
parameter would not be recognized as a context, and the whole function would go
unchecked.

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
The `tracer` type in `internal/chain/tracing.go` does it. Phi nodes are in
`phi.go`, and the stores behind a load are found by `store.go`.

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

Each tracer type knows the calls that set its context (`isCtxSetBy`):

| Tracer | Context set by |
|--------|----------------|
| `tracerEvent` | `Event.Ctx(ctx)`, `Context.Ctx(ctx)`, `zerolog.Ctx(ctx)` / `log.Ctx(ctx)` |
| `tracerLogger` | `zerolog.Ctx(ctx)` / `log.Ctx(ctx)` |
| `tracerContext` | `Context.Ctx(ctx)` |

> [!NOTE]
> `zerolog.Ctx(ctx)` returns the logger stored in `ctx`. It does not attach
> `ctx` to the events of that logger. The analyzer still accepts it: the code
> took its logger from the context, which is a deliberate use of it.

A call that turns one zerolog type into another is a handoff. Tracing goes on
from the call's receiver, with the receiver's tracer. Each handoff is declared
once, and `tracerHandoffs` lists which ones each tracer follows:

| Handoff | Call | Followed by |
|---------|------|-------------|
| `traceLoggerOfEvent` | `Logger.Info()`, `Logger.Debug()`, ... | `tracerEvent` |
| `traceContextOfLogger` | `Context.Logger()` | `tracerEvent`, `tracerLogger` |
| `traceLoggerOfContext` | `Logger.With()` | `tracerLogger`, `tracerContext` |

### Common SSA Patterns

`traceCommon` in `internal/chain/tracing.go` handles shared patterns:

- **Phi nodes** - Conditional assignments (all branches must have context)
- **UnOp** - Pointer dereferences
- **Alloc** - Local variable allocation (traces stored values)
- **FreeVar** - Closure captured variables
- **FieldAddr/Field** - Struct field access
- **Store tracking** - Values stored at addresses

#### Updates

A store whose value loads from the same address, such as
`l = l.With().Ctx(ctx).Logger()` once `l` has its address taken, is an update.
Real zerolog takes that address for every `Logger.Info()` call, since the
method has a pointer receiver. An update is not traced as a value of its own,
since it is built from the other stores.

An update can still set the context itself. It is accepted on its own when it
runs before every read of the local variable and no plain store can run after
it. An update on one branch only does not count.

#### Address Matching

Addresses are compared structurally, not by identity: SSA emits a fresh
`FieldAddr`/`IndexAddr` for every access, so the write and read halves of a
nested access (`h.inner.event`) never share a value.

#### Aggregate Copies

A composite literal is built in a temporary that is then copied into the
destination as a single value, so no store ever names the destination field:

```
t0 = local eventHolder (h)
t1 = local eventHolder (complit)
t2 = &t1.event
*t2 = v            // field initialized on the temporary
t5 = *t1
*t0 = t5           // whole struct copied into h
t6 = &t0.event     // the address we want the value of
```

When no store matches an address directly, `findStoresThroughAggregateCopy`
splits it into a root plus a chain of selections, follows the copy back to the
temporary, and replays the same chain there. Both shapes are handled, since
whether the compiler emits the temporary depends on the x/tools version.

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

## Directives

`internal/directive` reads comments the way mpyw/declscope and mpyw/errlogreturn do:

1. A trailing `// ...` is a reason. The comment is cut at the first `//` after the leading one, then parsed with `go/ast.ParseDirective`. So `//zerologlintctx:ignore//reason` is a bare ignore.
2. Only `//zerologlintctx:name [args]` is a directive. Another comment that starts with `zerologlintctx:` is malformed.
3. `ignore` is the only name. Any other name, such as `ignre` or `ignore-reason`, is an unknown directive. `go/ast` takes the name up to the first space, so these parse as directives and would otherwise do nothing without a report.
4. `ignore` takes no argument. A reason after ` - ` is kept for compatibility. Other text, such as `//zerologlintctx:ignore intentionally detached`, is reported.

None of these reported comments silences anything (#60). Guessing what was meant would silence a report nobody asked to silence.

## Known Limitations

Due to SSA analysis constraints:

- **Helper function returns**: Can't track through interprocedural analysis (IIFE is supported)
- **Channel send/receive**: Can't trace through channels
- **Closure-modified capture**: Closure writes to outer variable
- **Slice and map elements**: An event read back from a slice or map is not traced, so one stored with `.Ctx(ctx)` is still reported

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
├── basic.go        # Simple cases, ignore directives and directive problems
├── evil.go         # Edge cases (nesting, closures)
├── evil_ssa.go     # SSA-specific patterns (Phi, FreeVar)
├── evil_logger.go  # Logger patterns, direct logging
├── with_logger.go  # WithLogger-specific tests
├── alias.go        # zerolog/context types reached through aliases
└── go127.go        # Go 1.27 syntax (promoted field keys, generic methods)
```

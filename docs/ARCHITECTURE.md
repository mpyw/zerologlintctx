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
| Direct logging on Logger without context | `IsLogger(recv) && ReturnsVoid(sig) && hasPrefix("Print")`, and the Logger has no context | `zerolog direct logging bypasses context; use Event chain with .Ctx(ctx)` |
| Direct logging via log package | `IsZerologLogPkg(pkg) && ReturnsVoid(sig) && hasPrefix("Print")` | `zerolog direct logging bypasses context; use Event chain with .Ctx(ctx)` |

The message names the first usable `context.Context` parameter, or `*name`
for a `*context.Context` one. A parameter whose type is a type parameter
counts when the constraint is, or embeds, `context.Context`, as for
`func f[C context.Context](ctx C)`. A closure without one of its own takes the name
from the function it is declared in. Where that name means something else at
the call, such as a parameter of the closure or a local variable that shadows
it, the context cannot be passed there, and nothing is reported.

Every call is checked the same way, whether it is a plain call, a `go`
statement or a `defer` statement. `calleeOf` in `callee.go` resolves the
declared function a call runs, and its receiver, in each of the three forms a
method can take:

| Form | SSA | Receiver |
|------|-----|----------|
| `e.Msg("x")` | `(*Event).Msg(e, "x")` | `Args[0]` |
| `m := e.Msg; m("x")` | `(*Event).Msg$bound(e)`, then a call to it | `Bindings[0]` |
| `f := (*Event).Msg; f(e, "x")` | `(*Event).Msg$thunk(e, "x")` | `Args[0]` |

Tracing resolves each call in the chain the same way, so `info := l.Info;
info()` is traced back to `l`.

A method expression of a method promoted through an embedded field, such as
`myEvent.Msg(me, "x")` for `type myEvent struct{ *zerolog.Event }`, passes
the struct in `Args[0]`. `calleeOf` records the embedded fields that lead to
the receiver, and tracing reads it from the stores to those fields. An
embedded pointer partway, as in `type outer struct{ *myEvent }`, is followed
to each struct that can be stored there. A `Ctx` call in place on the
embedded event, such as `me.Ctx(ctx)` with its result unused, counts as it
does for an event in memory.

A conversion between types with the same underlying type, such as
`zerolog.Logger(d)` for `type definedLogger zerolog.Logger`, keeps the value,
and tracing goes on through it.

### Type-Based Analysis

The analyzer uses **return type checking** instead of method name hardcoding for most detection:

| Transition | Detection Method | Examples |
|------------|------------------|----------|
| Logger → Event | `ReturnsEvent(sig)` | `Info()`, `Debug()`, `Err()`, `WithLevel()` |
| Logger → Context | `ReturnsContext(sig)` | `With()` |
| Context → Logger | `ReturnsLogger(sig)` | `Logger()` |
| Event → Event | receiver is the traced type | `Str()`, `Int()`, `Ctx()`, etc. |
| Context → Context | receiver is the traced type | `Str()`, `Int()`, `Ctx()`, etc. |
| Logger → Logger | receiver is the traced type | `Level()`, `Sample()`, `Hook()`, `Output()` |

This approach automatically handles new zerolog methods without code changes.

Two methods are known by name, since zerolog's source, not their signature,
tells what they do with the context (`callee.go`):

| Method | What it does | How tracing takes it |
|--------|--------------|----------------------|
| `Event.CreateDict()` | Returns a new event for a dict, not its receiver | A `Ctx` call on the dict does not count for the parent (`returnsSameEvent`) |
| `Logger.Output(w)` | Builds a new logger with `zerolog.New(w)`, and drops the context `Context.Ctx` set | Tracing goes on past it with a detached tracer (`dropsLoggerContext`) |

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

`Logger.Print` builds its event from the Logger, as `Logger.Info` does, so the
event takes the Logger's context. The receiver is traced with `tracerLogger`
(`loggerHasCtx`), and the call is reported only when the Logger has no
context:

```go
l := base.With().Ctx(ctx).Logger()
l.Print("x") // OK
```

The `Print` functions of `zerolog/log` use the global Logger. It is not
traced, so they are always reported.

## SSA Tracing

The tracing system follows SSA values backwards to find if context was set.
The `tracer` type in `internal/chain/tracing.go` does it. Phi nodes are in
`phi.go`, and the stores behind a load are found by `store.go`.

One tracer serves a whole package. It keeps each value it found to have
context, so it is not worked out again for the next call that reaches it,
or for a closure that reaches a value of the function it is declared in.

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
| `tracerLoggerDetached`, `tracerContextDetached` | `zerolog.Ctx(ctx)` / `log.Ctx(ctx)` |

The detached tracers trace a Logger, and the Context it is built from, past
`Logger.Output`. Only a logger taken from a context counts there, as the note
below says. They follow the same handoffs between Logger and Context.

A `Ctx` call given a fresh `context.Background()` or `context.TODO()` does not
set the context. It replaces the context in scope, so tracing stops there and
reports the chain. `isFreshContext` in `context.go` takes the semantics of
goroutinectx: every value the argument can be must be a call of one of the
two. It sees through a Phi and a conversion. For a load, every store that
reaches it must be fresh. This holds for a local variable, a pointer
parameter, and a field of one. A global is not followed. A context derived from one, such as
`context.WithoutCancel(context.Background())`, is not fresh.

> [!NOTE]
> `zerolog.Ctx(ctx)` returns the logger stored in `ctx`. It does not attach
> `ctx` to the events of that logger. The analyzer still accepts it: the code
> took its logger from the context, which is a deliberate use of it.

`Event.Ctx` sets the context on the event it runs on, and returns that same
event. So an event whose chain has no `Ctx` call is still accepted when one
runs on it before the terminator on every path, with its result unused or not
(`traceCtxSetInPlace`):

```go
e := logger.Info()
e.Ctx(ctx)
e.Msg("x") // OK
```

A deferred terminator runs at each return after the `defer` statement, so for
one a `Ctx` call that runs on every path from the `defer` to a return counts
too.

A call that turns one zerolog type into another is a handoff. Tracing goes on
from the call's receiver, with the receiver's tracer. Each handoff is declared
once, and `tracerHandoffs` lists which ones each tracer follows:

| Handoff | Call | Followed by |
|---------|------|-------------|
| `traceLoggerOfEvent` | `Logger.Info()`, `Logger.Debug()`, ... | `tracerEvent` |
| `traceContextOfLogger` | `Context.Logger()` | `tracerEvent`, `tracerLogger` |
| `traceLoggerOfContext` | `Logger.With()` | `tracerLogger`, `tracerContext` |
| `traceDetachedContextOfLogger` | `Context.Logger()` past `Output` | `tracerLoggerDetached` |
| `traceDetachedLoggerOfContext` | `Logger.With()` past `Output` | `tracerLoggerDetached`, `tracerContextDetached` |

### Common SSA Patterns

`traceCommon` in `internal/chain/tracing.go` handles shared patterns:

- **Phi nodes** - Conditional assignments (all branches must have context)
- **UnOp** - Pointer dereferences
- **Alloc** - Local variable allocation (traces stored values)
- **FreeVar** - Closure captured variables
- **Parameter** - A parameter of a func literal that is only called where it is written, as an IIFE is, takes the argument of every such call, including calls from closures that capture the variable it is kept in, and calls of a variable or a Phi that may hold another func too. A parameter of a declared function, or of a func literal passed on, is not known
- **FieldAddr/Field** - Struct field access
- **Store tracking** - The stores that reach each read of an address

#### Stores That Reach a Read

A variable whose address is taken is read from memory. Real zerolog takes that
address for every `Logger.Info()` call, since the method has a pointer
receiver, so `l.Info()` reads `l` through `&l`.

`storesReaching` in `store.go` walks the control flow back from the read, and
stops each path at the first store to the address. Only the stores where a
path stops can be what the read sees, and every one must have context:

| Code before `l.Info()` | Stores that reach it | Reported |
|------------------------|----------------------|----------|
| `l := base` then `l = l.With().Ctx(ctx).Logger()` | The second | No |
| The second on one branch of an `if` only | Both | Yes |
| The second on both branches of an `if` | Both updates | No |

A store whose value loads from the same address, such as
`l = l.With().Str("k", "v").Logger()`, is an update. It is traced like any
other value: its load is a read, with stores of its own that reach it. An
update in a loop reaches itself, and is skipped there, as a Phi edge that
leads back to the Phi is.

| Where a path stops | What it carries |
|--------------------|-----------------|
| A store to the address | The value stored |
| A store of the whole struct or array that holds it | What reaches the same field of the aggregate copied in |
| The declaration of a local variable | Its zero value, which has nothing to trace |
| The entry of the function, for a pointer parameter | The caller's value, which is not known, so it is reported |
| The entry of a func literal only called where it is written, for a pointer parameter | What is at the same field of each argument when its call runs |
| A copy of a struct or array parameter of such a func literal | What is at the same field of each argument when it is loaded |
| The entry of a closure, for a variable it captures | What can be in the variable when the closure runs: what reaches the `make closure` and the stores after it, or what reaches the returns after a `defer` of it |

This holds for a local variable, a pointer parameter, a captured variable, and
a field or constant element of one, wherever the read runs. A field of a
global, or of a struct reached through a pointer the function loads, can be
changed elsewhere. For those, every store in the function must have context.
Each load of the same pointer, such as a receiver that a closure captures,
which is loaded again at each use, is taken for the same pointer when the same
stores to the pointer reach both loads, and so is each read of the same
pointer field of a struct value, such as `h.m`. After `p = b`, a load of `p`
is another pointer than the one before it.

A field read through a Phi of pointers, such as `h.l` after
`h := &holder{...}; if cond { h = &holder{...} }`, with no store through the
Phi itself, has what each variable the Phi may point to holds there. Each one
is walked from the read along the paths on which the Phi points to it: at the
top of the block of the Phi, the walk goes on through the edges that hold it
only. Every value that reaches the read must have context. A Phi that may
point to more than a few variables, or to anything but a variable, a pointer
parameter or a captured variable, is not walked.

When the read is not known, such as for an address that a Phi picks, every
read of the address counts. An instruction that passes the address on, such
as a closure that captures it, may read it later, so every store after it
counts too. A closure that is only deferred runs at the returns after the
`defer` statement, so the stores that reach those returns count instead. A
closure that is only called runs at each call, and the stores that reach the
calls count.

`Logger.UpdateContext(fn)` neither stops a path nor reads the Logger.
zerolog keeps only the fields `fn` writes, and drops the context
`Context.Ctx` sets in it, so the Logger has the context it had before.

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

An address is split into a root plus a chain of selections. When the walk
back from a read meets a store of the whole root, `storeWalk.copied` follows
the copy back to the temporary, and walks on from the load of it with the same
chain. `findStoresThroughAggregateCopy` does the same for an address whose
stores are not walked. Both shapes are handled, since whether the compiler
emits the temporary depends on the x/tools version.

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

An ignore at the end of a line of code reaches that line only. An ignore on a line of its own reaches the line below. A report sits at the `(` of the reported call, so a chain that spans several lines is also reached from the line where its call expression starts. `directive.Scan` records that start for every call, and whether each ignore follows code on its line, from the file's AST.

None of these reported comments silences anything (#60). Guessing what was meant would silence a report nobody asked to silence.

## Known Limitations

Due to SSA analysis constraints:

- **Helper function returns**: Can't track through interprocedural analysis (IIFE returns and arguments are supported)
- **Channel send/receive**: Can't trace through channels
- **Closure-modified capture**: Closure writes to outer variable
- **In-place `Ctx` through another reference**: `e.Ctx(ctx)` with its result unused is not seen by a terminator that reaches the event through another reference: a closure that captures `e`, a func literal `e` is passed to, or another variable `e` was copied to. The same variable or field in memory is followed: a `Ctx` call on an earlier load of it counts, unless another event is stored in between. For a method expression of a promoted method, one through an embedded pointer partway is not seen
- **Panics**: A deferred call is checked at the returns after its `defer` statement, not at the points where a panic could run it
- **Slice and map elements**: An event read back from a slice or map is not traced, so one stored with `.Ctx(ctx)` is still reported
- **Array elements with a variable index**: An element read with a variable index, as a range over an array reads it, is not matched to the stores of each element, so one stored with `.Ctx(ctx)` is still reported
- **Writes through another reference**: A write through a pointer passed to a function, another slice of the same array, or another pointer parameter is not seen, so the value stored before it is taken as the one read
- **Fresh contexts not seen**: A fresh context given to `Ctx` in place, with its result unused, does not replace the context the event had. A fresh context in a variable a closure captures is not taken as fresh

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

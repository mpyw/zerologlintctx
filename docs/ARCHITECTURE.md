# Architecture

This document describes the internal architecture of zerologlintctx.

## Overview

zerologlintctx uses SSA (Static Single Assignment) form analysis to track zerolog chains and detect missing context propagation. The analyzer is built on the [`go/analysis`](https://pkg.go.dev/golang.org/x/tools/go/analysis) framework.

```
zerologlintctx/
├── cmd/zerologlintctx/        # CLI entry point (singlechecker)
├── plugin/                    # golangci-lint module plugin
├── internal/                  # Core analysis logic
│   ├── chain/                 # zerolog semantics: the tracer and the checker
│   │   ├── checker.go         # Checker: checks each call, go and defer
│   │   ├── callee.go          # Classifies the function a call runs by zerologapi
│   │   ├── context.go         # Which functions have a context in scope, fresh contexts
│   │   ├── tracing.go         # Trace states, value tracing
│   │   ├── event.go           # Which event a value is, and Event.Ctx in place
│   │   ├── verdict.go         # Yes, no or neutral, and how they combine
│   │   └── memo.go            # The verdict of each value, and the traces in progress
│   ├── ssaflow/               # Where a value comes from; knows nothing of zerolog
│   │   ├── doc.go             # Package documentation
│   │   ├── resolve.go         # Resolver: what a read can see, one source at a time
│   │   ├── store.go           # The stores that reach a read
│   │   ├── closure.go         # Index of closures and func literals
│   │   ├── callee.go          # The declared function a call runs, and its receiver
│   │   ├── match.go           # Whether two addresses are the same
│   │   ├── place.go           # An address as a root and its selections
│   │   ├── walk.go            # Walks the paths of the control flow
│   │   ├── model.go           # Model: what a library does with memory
│   │   └── value.go           # SSA plumbing the other files stand on
│   ├── directive/             # Comment directive handling
│   │   └── ignore.go          # //zerologlintctx:ignore parsing, directive problems
│   └── zerologapi/            # What each zerolog function and method does
│       └── zerologapi.go      # Classify, the exceptions, context types, Model
├── testdata/src/              # Test fixtures and library stubs
├── analyzer.go                # Public analyzer, directive reporting
└── analyzer_test.go           # Integration tests
```

### Packages

| Package | Knows | Imports |
|---------|-------|---------|
| `internal/zerologapi` | The zerolog API: what each function and method does, and what zerolog does with memory (`Model`) | Nothing of this module |
| `internal/ssaflow` | Where a value of a function in SSA form comes from: places, stores, closures, walks of the control flow | Nothing of this module |
| `internal/chain` | When a chain or a Logger has context: the tracer, the checker, `Event.Ctx` in place, fresh contexts | `ssaflow`, `zerologapi` |
| `internal/directive` | The `//zerologlintctx:` comments | Nothing of this module |

`ssaflow` does not import `zerologapi`, and no code in it names zerolog. What
it must know of a library, it asks a `Model` (see
[The Library Model](#the-library-model)). `chain` passes it
`zerologapi.Model`.

## Detection Logic

### What Gets Detected

| Pattern | Condition | Message |
|---------|-----------|---------|
| Event chain missing `.Ctx()` | `KindTerminator`: a method of Event that returns nothing | `zerolog call chain missing .Ctx(<name>)` |
| Direct logging on Logger without context | `KindDirectLog` on a Logger, and the Logger has no context | `zerolog direct logging bypasses context; use Event chain with .Ctx(<name>)` |
| Direct logging via log package | `KindDirectLog` on a function of `zerolog/log` | `zerolog direct logging bypasses context; use Event chain with .Ctx(<name>)` |

`<name>` is the first usable `context.Context` parameter, such as `ctx` or
`c`, or `*name` for a `*context.Context` one, such as `*pc`. A parameter whose type is a type parameter
counts when the constraint is, or embeds, `context.Context`, as for
`func f[C context.Context](ctx C)`. A closure without one of its own takes the name
from the function it is declared in. Where that name means something else at
the call, such as a parameter of the closure or a local variable that shadows
it, the context cannot be passed there, and nothing is reported.

Every call is checked the same way, whether it is a plain call, a `go`
statement or a `defer` statement. `ssaflow.CalleeOf` resolves the declared
function a call runs, and its receiver, in each of the three forms a method
can take, and `calleeOf` in `internal/chain/callee.go` classifies it with
`zerologapi.Classify`:

| Form | SSA | Receiver |
|------|-----|----------|
| `e.Msg("x")` | `(*Event).Msg(e, "x")` | `Args[0]` |
| `m := e.Msg; m("x")` | `(*Event).Msg$bound(e)`, then a call to it | `Bindings[0]` |
| `f := (*Event).Msg; f(e, "x")` | `(*Event).Msg$thunk(e, "x")` | `Args[0]` |

Tracing resolves each call in the chain the same way, so `info := l.Info;
info()` is traced back to `l`.

A closure that captures `info` loads it from the variable, and calls what it
loads. `Closures.CalleeOf` takes the one value stored to the variable as the
callee, and a method value's receiver is then what the method value is bound
to, in the function that makes the closure. It is read where the closure runs:
its only call, `go` or `defer` statement, or else the `MakeClosure`. The same
goes for the struct a method expression of a promoted method is passed, when
the closure loads it from a variable it captures. A method expression takes its
receiver from the call, so `msg := (*zerolog.Event).Msg; func() { msg(e, "x")
}()` is traced to `e`. A `Ctx` call in place counts both in the closure, before
the terminator, and in the function that makes the closure, before it runs.

A method expression of a method promoted through an embedded field, such as
`myEvent.Msg(me, "x")` for `type myEvent struct{ *zerolog.Event }`, passes
the struct in `Args[0]`. `CalleeOf` records the embedded fields that lead to
the receiver, and tracing reads it from the stores to those fields. An
embedded pointer partway, as in `type outer struct{ *myEvent }`, is followed
to each struct that can be stored there. A plain call or method value of the
same promoted method reads the event past a load of the pointer, which is
followed in the same way (see [Pointers Read From Memory](#pointers-read-from-memory)). A `Ctx` call in place on the
embedded event, such as `me.Ctx(ctx)` with its result unused, counts as it
does for an event in memory, through an embedded pointer partway too.

A conversion between types with the same underlying type, such as
`zerolog.Logger(d)` for `type definedLogger zerolog.Logger`, keeps the value,
and tracing goes on through it.

### The zerolog API

`zerologapi.Classify` tells what a zerolog function or method does. The tracer
and the checker ask it, and match no names of their own. It returns a
`Method`:

| Field | Meaning |
|-------|---------|
| `Kind` | What a call does (see below) |
| `Recv` | The zerolog type of the receiver: Event, Logger, Context, or none |
| `Result` | The zerolog type of the only result, or none |
| `Keeps` | The sources of the receiver's context that the result still has |
| `SameAsRecv` | The result is the receiver itself, not a value made from it |
| `Source` | The context a `KindSource` sets |

Most of it comes from the signature:

| Kind | Rule | Examples |
|------|------|----------|
| `KindTerminator` | A method of Event that returns nothing | `Msg`, `Msgf`, `MsgFunc`, `Send` |
| `KindDirectLog` | A `Print*` method of Logger, or function of `zerolog/log`, that returns nothing, and `Logger.Write` | `Print`, `Printf`, `Println`, `Write` |
| `KindChain` | A method that returns its receiver's type | `Event.Str`, `Context.Str`, `Logger.Level` |
| `KindHandoff` | A method that returns another zerolog type | `Logger.Info`, `Logger.With`, `Context.Logger` |
| `KindOther` | Anything else: a function, or a method that returns another type | `zerolog.New`, `log.Info`, `Logger.GetLevel` |

A method of Event that returns an event returns its receiver. The methods of
Logger and Context have a value receiver, and return a copy.

`zerologapi.Model` tells `ssaflow` what zerolog does with memory (see
[The Library Model](#the-library-model)).

A small table of exceptions holds what zerolog's source, not the signature,
tells. Each row cites zerolog v1.35.1, and `zerologapi_test.go` tests each:

| Function or method | Kind | Why |
|--------------------|------|-----|
| `Event.Ctx(ctx)` | `KindSource`, `SourceEvent` | Sets the context of the event, and returns it |
| `Context.Ctx(ctx)` | `KindSource`, `SourceContext` | Sets the context of the Logger the Context builds |
| `zerolog.Ctx(ctx)`, `log.Ctx(ctx)` | `KindSource`, `SourceStored` | Returns the Logger stored in `ctx` |
| `Event.CreateDict()` | `KindChain`, not `SameAsRecv` | Returns a new event for a dict, with the context of its receiver |
| `Context.CreateDict()` | `KindOther` | Returns a dict event. Tracing does not follow it back (a limitation) |
| `Logger.Output(w)` | `KindChain`, keeps `SourceStored` only | Builds a new Logger with `zerolog.New(w)`, and drops the context `Context.Ctx` set |
| `Logger.Write(p)` | `KindDirectLog` | Logs with `l.Log() ... Msg`, as `Print` does. Its name does not start with `Print` |
| `Logger.UpdateContext(fn)` | `KindInPlaceUpdate` | Changes the fields of the Logger in place, but not its context |

Type identity checks resolve aliases (`types.Unalias`) before comparing the
package path and type name, since go/types represents every alias declaration
as a `*types.Alias` node. Without that, a `type MyCtx = context.Context`
parameter would not be recognized as a context, and the whole function would go
unchecked.

### Exception: Direct Logging

For direct logging, `Classify` uses a **name prefix check**:

```go
// Detected (Print* prefix + void return)
logger.Print(...)
logger.Printf(...)
logger.Println(...)

// NOT detected (void but not Print*)
logger.UpdateContext(...)  // Configuration, not logging
```

This is necessary because `UpdateContext` also returns void but is not a logging method.

`Logger.Write` does not start with `Print`, so the prefix check does not find
it. It logs with `l.Log() ... Msg`, so a row of exceptions makes it direct
logging, as `Print` is. A use through `io.Writer`, such as `fmt.Fprint(l, "x")`, is not
seen.

`Logger.Print` builds its event from the Logger, as `Logger.Info` does, so the
event takes the Logger's context. The receiver is traced in the `traceLogger` state
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
The `tracer` type in `internal/chain/tracing.go` does it. What a read from
memory can see is found by `internal/ssaflow`.

`ssaflow.Resolver`, in `resolve.go`, turns what `store.go` found into
sources, one at a time: a variable a closure captures becomes what the
variable bound where the closure is made holds when it runs, and a parameter
of a func literal becomes what each call passes for it, until only values
stored, or unknown, are left. The tracer judges each source, and every one
must have context. Each `Resolver` is told how its caller marks a parameter
in progress (`Enter`), so the tracer keeps that in its memo, and the other
users keep a set of their own.

One tracer serves a whole package. It keeps the verdict of each value it
traced, so it is not worked out again for the next call that reaches it,
or for a closure that reaches a value of the function it is declared in.

### Verdicts

A trace answers with a `verdict`, in `verdict.go`:

| Verdict | Meaning |
|---------|---------|
| `yes` | The value has context |
| `no` | The value has no context, or it is not known |
| `neutral` | The value adds no source: a nil event, which logs nothing, or a value whose trace is in progress |

A value that may be one of several sources, such as a Phi, the stores that
reach a load, or the calls of a func literal, combines them with `all`:

| Sources | `all` |
|---------|-------|
| One is `no` | `no`, and the rest are not traced |
| At least one `yes`, the rest `neutral` | `yes` |
| None, or all `neutral` | `neutral` |

`some` is the other way round, for ways to find the same value, any of which
will do: the value an IIFE returns and the argument it stands for. At the
terminator, only `yes` counts.

#### Cycles

A value reached again while its trace is in progress is `neutral`, whatever
reaches it: an edge of a Phi in a loop, a store in a loop that reaches
itself, or a recursive call of a func literal. The edge that leads back adds
no source that the trace in progress does not see on its own:

```go
e := logger.Info().Ctx(ctx)
for range n {
    if cond {
        e = logger.Warn() // traced: no context
    }
    e = e.Str("k", "v")   // leads back to the loop's Phi: neutral there
}
e.Msg("x") // reported
```

`traceMemo` in `memo.go` keeps the verdicts, and marks the values in
progress, per trace state. A verdict that depends on a value in progress,
which is `neutral` only for now, is kept with the frame of that value, and
used again only while the frame is on the stack: a loop of many Phis is then
traced once, not once per path. A verdict that depends on no value in
progress below its own frame, `yes` or `no`, is kept for good.

### Walking the Control Flow

`ssaflow`'s `walk.go` walks the paths of a function block by block, from a
point before an instruction. `walkBack` goes backwards, latest instruction
first, and `WalkFwd` goes forwards. The caller's visit function tells what each path
does at an instruction: go on, stop there, or end the whole walk. The walk
takes each block once, and calls back when a path reaches the entry of the
function.

| Walk | Direction | A path stops at | Kept to |
|------|-----------|-----------------|---------|
| `storeWalk.walkBefore`: stores that reach a read | Back | A store to the address, or its declaration | Through a Phi of pointers, the edges that hold the variable picked |
| `storeWalk.walkDeferred`: stores that reach the returns after a `defer` | Back | The same, or the `defer` | The blocks on a path through the `defer` (`walkThrough`) |
| `storeWalk.addStoresAfter`: stores after an escape of a variable declared in a loop | Forward | Its declaration on the next iteration | All |
| `eventQuery.ctxRunsBefore`: an `Event.Ctx` call before a terminator | Back | A `Ctx` call on the event, or a store of another event where it is read | Through a Phi it is read through, the edges that carry the event |
| `eventQuery.ctxRunsAfterDefer`: an `Event.Ctx` call after a deferred terminator | Forward | A `Ctx` call on the event | All |

`WalkReachable` answers a narrower question: whether a path from a point
reaches another without running a given instruction. The in-place `Ctx`
check asks it to tell whether a value kept at a Phi or in memory still holds
the event of the latest run of a call in a loop (see
[Events made in a loop](#events-made-in-a-loop)).

`WalkBackWith` carries a state on each path. A block is walked once in each
state. `walkBefore` uses it to keep, on each path, the Phi that must still
point to the variable picked.

### Closures and Func Literals

The tracer also keeps an `ssaflow.Closures` index, in `closure.go`, which it
shares with its resolvers. It indexes the tree
of an outermost function, the function and the func literals declared in it at
any depth, the first time a query reaches it, and keeps each answer:

| Query | Answer |
|-------|--------|
| `makersOf(fn)` | The `MakeClosure` instructions of `fn` in the function it is declared in |
| `bindingOf(fv)` | The value bound to a free variable where its closure is made |
| `aliasesOf(alloc)` | A local variable, and the free variables that stand for it in the closures that capture it, at any depth |
| `CallsOf(fn)` | Every call, go and defer statement that may run a func literal, through variables and Phis, or not ok when it may run where it cannot be seen |
| `funcsHeldBy(v)` | The values stored to a variable that is only stored to, loaded and captured |

SSA makes a func literal once, where it is written, so `makersOf` gives at
most one. `bindingOf` takes the last, and resolving a free variable takes
each, every one of which must give a value with context. The two rules agree
as long as that holds.

`store.go` asks only how a closure that captures an address runs where it is
made, `closureRunsOf`: the calls, go and defer statements of it, without
following it through a variable.

### Trace State

A value is traced in a `traceState`: its zerolog type, and the sources of
context that still count for it. Tracing starts in one of two states:

| State | Starts at | Sources that count |
|-------|-----------|--------------------|
| `traceEvent` | The receiver of a terminator | All |
| `traceLogger` | The receiver of a direct logging call (`loggerHasCtx`) | All |

At a call of a declared function, tracing asks `Classify`, then:

| The call | What tracing does |
|----------|-------------------|
| A `KindSource` whose source counts | Found, unless the context is fresh (see below) |
| A method of the traced type | Goes on from the receiver, in the same state. A `KindChain` keeps only the sources its result keeps |
| A `KindHandoff` that returns the traced type | Goes on from the receiver, as the receiver's type |
| For an event, a `KindHandoff` that returns a Logger | The same. This is for a value of another type, as below |
| Anything else | Not found |

`Logger.Output` keeps only `SourceStored`. So past it, a context that
`Context.Ctx` set does not count, and a Logger from `zerolog.Ctx(ctx)` still
does. The set stays narrowed through the handoffs between Logger and Context.

The first argument of an unknown function is traced in the state of the
function's result. A method expression read from a map is one:

```go
levels := map[string]func(*zerolog.Logger) *zerolog.Event{"info": (*zerolog.Logger).Info}
l := base.With().Ctx(ctx).Logger()
levels[level](&l).Msg("x") // OK: l is traced as an event
```

`l` is a Logger, traced as an event. An event is made from a Logger, so
`Context.Logger()` is followed back to the Context. A Logger and a Context
need no such case. The handoff that makes either one is a method of the
other, and tracing follows a method of the traced type anyway.

> [!NOTE]
> `zerolog.Ctx(ctx)` returns the logger stored in `ctx`. It does not attach
> `ctx` to the events of that logger. The analyzer still accepts it: the code
> took its logger from the context, which is a deliberate use of it.

### Fresh Contexts and Ctx in Place

A `Ctx` call given a fresh `context.Background()` or `context.TODO()` does not
set the context. It replaces the context in scope, so tracing stops there and
reports the chain. `isFreshContext` in `context.go` takes the semantics of
goroutinectx: every value the argument can be must be a call of one of the
two. It sees through a Phi and a conversion. For a load, every source that
`ssaflow.Resolver` finds must be fresh: the stores that reach it, in a local
variable, a pointer parameter, a variable a closure captures, or a field of
one, through where the closure is made and the calls of a func literal. A
global is not followed. A context derived from one, such as
`context.WithoutCancel(context.Background())`, is not fresh.

The tracer leaves out a store that a walk of the function does not see. For a
fresh context, that would report a context that is not fresh, so such a
variable is not fresh: one that a closure stores to, other than the one read
in and the functions it is declared in, or whose address is passed on, such as
to a call:

```go
bg := context.Background()
func() { bg = ctx }()
l.Info().Ctx(bg).Msg("x") // OK: a closure stores to bg
```

`Event.Ctx` sets the context on the event it runs on, and returns that same
event. So an event whose chain has no `Ctx` call is still accepted when one
runs on it before the terminator on every path, with its result unused or not
(`eventIndex.eventCtxSetInPlace` in `event.go`):

```go
e := logger.Info()
e.Ctx(ctx)
e.Msg("x") // OK
```

The check is one query over the identity of the event. The sources of a
value are the events it can be. `eventOrigin` walks back through the methods
whose result is their receiver (`SameAsRecv` in the API table), such as
`Event.Str` and `Event.Ctx`, to the call that made the event. A Phi has the
sources of its edges. A read from memory has the sources of what
`ssaflow.Resolver` finds there. An event the function cannot tell, such as the caller's through
a pointer parameter, is a source of its own for each place it is read from.
So is one behind a pointer that is not in a variable, whose stores the
resolver cannot follow, such as `h.e` for a holder `h` a call returns, or
`(*pp).e`. Two reads through the same pointer, as `addressesMatch` takes
them, are of the same place (`ssaflow.MatchedPlaces`). A pointer that is a
global, or is read from one, is not: another goroutine may change what is
there between the two reads, so `gh.e.Ctx(ctx); gh.e.Msg("x")` is reported.

| Value | Sources |
|-------|---------|
| `logger.Info()` | Itself |
| `e.Str("k", "v")`, `e.Ctx(ctx)` | Those of `e` |
| A Phi | Those of each edge but nil |
| A load, or a field selected from a load of a struct | Those of each value stored that reaches it |
| A field selected from a Phi of struct values | Those of the same field of each load an edge leads to |
| The receiver of a method expression of a promoted method | Those of each value stored at the embedded fields |
| Anything else | Itself |

Each source of the terminator's receiver must then have a `Ctx` call, given
a context that is not fresh, on every path from the source to the
terminator. A `Ctx` call counts for a source when it runs on a value that
can be that source, and on no event the receiver cannot be. So a call on
another read of the same variable, or on another variable the event was
copied to, counts:

```go
h := holder{logger.Info()} // type holder struct{ *zerolog.Event }
h.Ctx(ctx)                 // t4 = *t1 ; t5 = t4.Event ; Ctx(t5, ctx)
h.Msg("x")                 // t6 = *t1 ; t7 = t6.Event ; Msg(t7, "x"), OK
```

The walk goes back from the terminator. A path ends at a `Ctx` call on the
source, and fails at the instruction that makes it. One that leads back to
the entry of the function fails when the function does not make the source,
as for a parameter. A path that does not carry the source needs no call for
it: it takes an edge of a Phi the receiver is read through that is nil,
another event, or one with context of its own, or it stores such a value
where the receiver is read from.

```go
e := logger.Info()
e.Ctx(ctx)              // in place, on the first iteration
for range n {
    e.Msg("x")          // OK
    e = logger.Warn().Ctx(ctx) // of its own, on the later ones
}
```

A func literal called where it is made, as an IIFE is, runs at each call,
and so does each `Ctx` call it makes on every run, one whose block dominates
every return of the func literal. Such a `Ctx` call counts at the call of
the func literal, with the events it runs on there (`addClosureCtxCalls`). A
`go` or `defer` statement runs the func literal at another time, and is not
such a call:

```go
e := logger.Info()
func() { e.Ctx(ctx) }() // counts here
e.Msg("x")              // OK
```

An event the func literal cannot tell, at a place it reads through a pointer
it loads from a variable it captures, such as the caller's event at `h.e`
for a pointer parameter `h`, or an element of a captured slice, is taken at
the call for the event at the same place through the variable bound there.
That place goes through a load of the variable that the same stores reach as
reach the call, and the func literal must not store to the variable before
its own load (`MatchedPlaces.KeyAtRun`):

```go
func f(ctx context.Context, h *holder) {
    func() { h.e.Ctx(ctx) }() // the caller's event at h.e
    h.e.Msg("x")              // OK: the same event
}
```

The index keeps the sources of each value it was asked about, as a set, and
the `Ctx` calls of each function, with those of the func literals it calls
in place, at each call. A function with no `Ctx` call is not
walked at all. The walk looks at a `Ctx` call only where it meets one, and
compares the sources of the call with those of the receiver once per query.
`TestEventQueryWorkIsQuadratic` counts that work for two shapes that took
more than quadratic time before (#247), and checks that it grows no faster
than the square of the number of terminators.

A `Ctx` call given a fresh context replaces the context of the event, so the
last `Ctx` call on a path decides. Such a call counts only when it runs on
the source alone, and on the same run of it (see below). A call that may run
on another event may leave the context as it was:

| Chain of the receiver | Reported when |
|-----------------------|---------------|
| Has no context | A path from the source has no `Ctx` call given the context in scope, or a fresh one after the last such call |
| Has context | A path ends with a fresh `Ctx` call on the source, with no `Ctx` call given the context in scope after it |

```go
e := logger.Info().Ctx(ctx)
if detached {
    e.Ctx(context.Background())
}
e.Msg("x") // reported: Background on one path
```

A function with no fresh `Ctx` call is not walked for a receiver whose chain
has context. A fresh `Ctx` call in a func literal called in place is not
looked at.

A deferred terminator runs at each return after the `defer` statement, so
for one the last `Ctx` call on the source on each path from the `defer` to a
return decides. A path with none keeps what the source had at the `defer`. A
path that makes the source again, in a loop, has another event there.

#### Events made in a loop

A call in a loop makes an event on each run, so the call alone does not name
one event. `eventRunGraph` in `event.go` follows a receiver back to each such
call with the point where each value on the way is used, and tells which
runs it can hold (`eventRuns`):

| Run | When | Example |
|-----|------|---------|
| Latest | No path kept the event past another run of the call | `e := logger.Info(); e.Msg("x")` |
| Earlier | The event was kept at a Phi, a load or in memory, and every path from there to the use runs the call again | `last`, kept from the last iteration |
| Any | A store at an index computed in the loop, read at another index | `evs[i] = logger.Info()` in a loop, then `evs[1]` |

A `Ctx` call counts for the terminator only when the two can hold the same
run. Any run can be the same as any other, except two elements at different
constant indices.

```go
var last *zerolog.Event
for range n {
    e := logger.Info()
    if last != nil {
        last.Ctx(ctx)   // an earlier run
        e.Msg("x")      // the latest run: reported
    }
    last = e
}
```

When the receiver holds only earlier runs, the walk back does not fail at
the latest run of the call. It goes on before it, where a `Ctx` call on any
run counts, and fails at the run before. So a `Ctx` call on each event before
it is kept for the next iteration counts.

Only calls that can run again after they run, in a loop, get this check, so
the graph is not built for the others.

### Common SSA Patterns

`traceCommon` in `internal/chain/tracing.go` handles shared patterns:

- **Phi nodes** - Conditional assignments (all branches must have context)
- **UnOp** - Pointer dereferences
- **Alloc** - Local variable allocation (traces stored values)
- **FreeVar** - Closure captured variables
- **Parameter** - A parameter of a func literal that is only called where it is written, as an IIFE is, takes the argument of every such call, including calls from closures that capture the variable it is kept in, and calls of a variable or a Phi that may hold another func too. A parameter of a declared function, or of a func literal passed on, is not known
- **Result of a call of a func literal** - What each return of the func literal returns, or, failing that, what this call passes for it. A parameter returned, or a chain of event methods on one, is the argument of this call (`traceArgumentReturned`). So is a parameter in what a result loaded from memory reads, such as `h.e` for `h := holder{e}`: the resolver is bound to the call (`Resolver.Bound`), and a parameter of the func literal, or what is at a field of one, is taken from this call alone, not from every call of it
- **FieldAddr/Field** - Struct field access. A field or element selected from a struct or array value loaded whole is read from the same selection of the address it is loaded from. One selected from a Phi of such values is read from each load an edge leads to, and every one must have context (`ssaflow.LoadedSelection`). Each read keeps the edges it is taken through, so that the in-place `Ctx` check still tells, at a Phi at the top of a loop, the event of an earlier iteration. A pointer read that way is followed by `storePointers` the same way:

  ```go
  w := wrapper{&a}         // type wrapper struct{ *zerolog.Logger }
  if cond {
      w = wrapper{&b}
  }
  w.Info().Msg("x")        // t18 = phi(t12, t16).Logger: reads t10.Logger and t14.Logger
  ```
- **Store tracking** - The stores that reach each read of an address

#### Stores That Reach a Read

A variable whose address is taken is read from memory. Real zerolog takes that
address for every `Logger.Info()` call, since the method has a pointer
receiver, so `l.Info()` reads `l` through `&l`.

`storesReaching` in `ssaflow`'s `store.go` walks the control flow back from
the read, and stops each path at the first store to the address. Only the
stores where a path stops can be what the read sees, and every one must have
context:

| Code before `l.Info()` | Stores that reach it | Reported |
|------------------------|----------------------|----------|
| `l := base` then `l = l.With().Ctx(ctx).Logger()` | The second | No |
| The second on one branch of an `if` only | Both | Yes |
| The second on both branches of an `if` | Both updates | No |

A store whose value loads from the same address, such as
`l = l.With().Str("k", "v").Logger()`, is an update. It is traced like any
other value: its load is a read, with stores of its own that reach it. An
update in a loop reaches itself, and is `neutral` there, as a Phi edge that
leads back to the Phi is (see [Cycles](#cycles)).

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
a field or element of one, wherever the read runs. A field of a global, or of
a struct reached through a pointer that is not in a variable, is walked for
the same way, through the stores that the function makes through the same
pointer (see [Pointers Not in a Variable](#pointers-not-in-a-variable)).

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
calls count. A Phi passes the address on only from the edges that are the
address, so the walk starts at the ends of those predecessors. On another
edge the Phi points elsewhere, and that edge may not even run the declaration
of a variable declared in a branch or a loop. For a variable declared in a
loop, the stores after an escape count only up to its declaration on the next
iteration, since the stores after that are to another variable.

#### Elements at a Variable Index

An element at an index that is not a constant, such as `arr[i]`, may be any
element. A store to an element, through another `IndexAddr` or the same, is
one of three kinds for a read of `arr[i]` (`Place.covers`):

| Store before the read | Kind | What the walk does |
|-----------------------|------|--------------------|
| `arr[i] = v`, or `*p = v` with `p := &arr[i]`, when `i` has one value (below) | Must | Records `v` and stops the path |
| `arr = [2]zerolog.Logger{a, b}`, a store of the whole array | Must | Follows the copy, at `[i]` of the array copied in |
| `arr[0] = v`, `arr[j] = v`, or `arr[i] = v` with `i` computed in a loop | May | Records `v`, and goes on |
| `arr[0] = v` for a read of `arr[1]` | None | Goes on |

The same holds the other way: for a read of `arr[0]`, a store to `arr[i]` is
a may store. A store that may write the element, or not, does not hide a
store before it, since the value before may still be the one read. What it
stores may be read too, so it must have context, like every other value that
reaches the read. A store that writes the element on every run stops the
path, as a store to a variable does:

```go
p := &arr[i]
*p = base.With().Ctx(ctx).Logger() // must: reaches the read
arr[0] = base                      // may: reaches it too, when i is 0
p.Info().Msg("x")                  // reported: arr[0] lacks ctx
```

Two elements at the same index are the same element only when the index has
one value wherever the walk meets it: a parameter, a free variable, or an
instruction outside a loop (`storeWalk.indexOnce`). Two loads of one local
variable are one index value too, when the variable is stored once, outside a
loop, before each load and each closure that captures it, and is otherwise
only loaded (`storeVariableOnce`). A parameter that a closure captures is such
a variable, so `s[i] = l; func() { s[i].Info() }()` reads the element stored. An index that a loop
computes, such as the index of a `for i := range arr`, has another value on
each iteration, so a store at it on an earlier iteration is to another
element. Each store at it is a may store. A store on the same iteration is
taken for one too, which loses precision, never a report (see
[Known Limitations](#known-limitations)).

A range over an array value reads each element at a variable index, from the
array loaded before the loop. The store of each element is a may store, so
every element must have context.

The same holds for an element of a slice that `make` returns, whose root is
a pointer that is not in a variable (see
[Pointers Not in a Variable](#pointers-not-in-a-variable)).

`Logger.UpdateContext(fn)` neither stops a path nor reads the Logger.
zerolog keeps only the fields `fn` writes, and drops the context
`Context.Ctx` sets in it, so the Logger has the context it had before. The
walk learns this from `Model.LeavesReceiver` (see
[The Library Model](#the-library-model)).

#### Places

`place.go` models an address as a `Place`: a root, the variable or pointer
the address is in, and a path of selections from it, outermost first.
`PlaceOf(addr)` builds one from the `FieldAddr` and `IndexAddr` instructions
of an address. A selection is a `sel`:

| Selection | `sel` |
|-----------|-------|
| A field, of an address or of a struct value | `field` |
| An element at a constant index | `index`, the constant |
| An element at any other index | `operand`, the index value, with no `index` |

`sel.same` is the test of whether two selections are the same, on their own.
Two fields are the same when their numbers are. Two constant elements are the
same when their values are, whatever the type of the constant. An element at
a variable index may be any element, so it is the same as no selection, not
even one at the same index.

`Place.covers` tells how much of a place a store to another place writes:
`coverMust`, `coverMay` or `coverNone`. It compares the selections as
`sel.same` does, and takes two elements for the same element when the
caller says that their indexes are one value. Any other pair of
elements, one of them at a variable index, is `coverMay`. See
[Elements at a Variable Index](#elements-at-a-variable-index).

`Place.Key` gives a place in a form that `==` can compare, for a map key. It
spells each element by its operand: a constant by its value and type, and a
variable index by the value. So the key, unlike `sel.same`, tells apart
`a[1]` and `a[uint8(1)]`, and takes two elements at the same variable index
for the same.

`isTrackedRoot` tells whether every store to a place with that root can be
seen in the function alone: a local variable, a parameter, or a variable a
closure captures. A global, or a pointer the function loads, can be changed
anywhere. A walk back from a read is only done for a tracked root, or for a
root that is a pointer read from memory, through what it points to (below).

#### Pointers Read From Memory

A struct reached through a pointer in a field, embedded or not, is at an
address whose root is the pointer, loaded from that field:

```
t1 = new pointedLogger (complit)
*&t1.l = base                 // the logger, stored in the struct pointed to
*&t0.p = t1                   // h := pointedLoggerHolder{p: t1}
t3 = *&t0.p                   // the pointer, read from h.p
Info(&t3.l)                   // h.p.l.Info(): t3 is t1
```

`storePointers` follows such a pointer to what it can point to: what the
stores that reach its read left in the field. The address is then walked for
as an address in each of those, with the same selections, from the read
(`t1.l` above), and every value that reaches it must have context. A Phi of
pointers is walked for each pointer it picks, as `storesReachingPicked` does,
and is not followed when it picks more than `storePickedMax`.

| The pointer is read | What it can point to |
|---------------------|----------------------|
| From a field or element of a tracked root, or of a struct value loaded from one | What the stores to that place left there |
| From what another such pointer points to | The same, through that pointer first |
| From a variable a closure captures, as the closure starts | What the variable bound where the closure is made holds when the closure runs (below) |
| And a store reaches it from a pointer parameter, or anything else not known | Not followed. The stores through the pointer itself are walked for (below) |

A nil pointer has nothing to read, and is left out. So is a path of the walk
that leads back to the entry of the function without making the struct a
pointer points to: the pointer is another one there. In the walk for each
variable, a store through another read of a pointer is a store to the
variable when that pointer can point to it: one that stops the path when it
can point to that variable only, and a may store otherwise. A pointer that
cannot be followed is taken to point elsewhere, as a write through another
reference is not seen (see [Known Limitations](#known-limitations)).

```go
h := pointedLoggerHolder{&pointedLogger{base}}
h.p.l = base.With().Ctx(ctx).Logger() // through another read of h.p: stops the path
h.p.l.Info().Msg("x")                 // not reported
```

`storePointers` keeps what each pointer can point to, and a pointer reached
again while it is followed is not followed again.

A closure that captures a pointer variable reads it as a free variable, and
loads the pointer at each use. What the free variable holds as the closure
starts is what the variable bound at the `MakeClosure` holds when the closure
runs (see `storesReachingClosure`). Each pointer found there is a target, and
the walk for it goes from the read back to the entry of the closure, then on
in the function that makes the closure, from where the closure runs: before
each call of a closure that is only called, at the returns of one that is only
deferred, and from where it is made, with every store after it, otherwise
(`storeWalk.walkOut`). A pointer that is not in a variable there, such as one
a call returns, is walked for as below, and is taken as not known when no
store through it is found.

```go
p := newHolder()
p.log = base.With().Ctx(ctx).Logger()  // t2 = *t0 ; *&t2.log = ..., and t2 is t1
go func() { p.log.Info().Msg("x") }()  // t0 = *p ; Info(&t0.log), and t0 is t1
```

#### Pointers Not in a Variable

Some pointers are not in a variable, or are read from one that can change
where the function does not see it: a pointer a call returns, a Phi of
pointers, a receiver that a closure captures, which is loaded again at each
use, `(*pp).log`, `arr[0].log`, `h.m.log`, or a global. When `storePointers`
cannot follow such a pointer, `storesReachingPointer` walks back from the
read for the stores through the same pointer, and stops each path at the
first one, as for a variable. A store through another load of the pointer is
a store through the same pointer when `addressesMatch` says so: the same
stores to where it is loaded from reach both loads (see
[Address Matching](#address-matching)).

```
t1 = *t0 ; *&t1.log = v   // reaches the read
t8 = *t0                  // where the pointer is made
Info(&t8.log)             // reads t8.log, and t8 is t1
```

| Where a path gets | What it does |
|-------------------|--------------|
| A store through the same pointer | Records the value, and stops (or goes on for a may store) |
| Where the pointer is made, a load | Goes on, up to a store of another pointer where it is loaded from, or the declaration of that variable |
| Where the pointer is made, any other instruction, such as a call or a Phi | Stops, and carries nothing |
| The entry of a closure, past where the pointer is made, when the closure reads it from the variables it captures | Goes on before each call of the closure, when it is only called where it is made (below) |
| The entry of the function | Carries nothing |

A path that carries nothing gets to where the function does not know what is
at the address: the callee that returned the pointer, the caller, or a time
before another pointer was put where it is read from, may have stored there.
Only the stores of the function itself are traced. When no path carries one,
the address is taken as not known to the walk, and the pointer itself is
traced. For a Phi of pointers with no store through it, the variables it
picks are walked instead (see [Stores That Reach a Read](#stores-that-reach-a-read)).

A pointer receiver is such a pointer too, when it is read from memory, as
`b.p` and `(*pl)` are. `Resolver.ResolvesThrough` takes it, so the call is the
read, and a store through the same pointer before it is what the call sees:

```go
b := box{l}
*b.p = zerolog.Ctx(ctx).With().Logger() // t1 = *&t0.p ; *t1 = ...
b.p.Info().Msg("x")                     // t2 = *&t0.p ; Info(t2): not reported
```

A closure that reads the pointer from the variables it captures, such as
`h.m.log` for a captured struct `h`, does not see where the pointer was
stored. A path that gets to the entry of the closure goes on before each call
of it, when it is only called where it is made. There, a store through a
pointer loaded from the variable the closure binds, at the same selections, is
a store through the same pointer when the same stores reach that load and the
call (`matchAcrossClosure`). A closure that is run by a `go` or `defer`
statement, or passed on, adds nothing.

```go
h.m.log = zerolog.Ctx(ctx).With().Logger() // t2 = *&t0.m ; *&t2.log = ...
func() { h.m.log.Info().Msg("x") }()       // t1 = *&h.m ; Info(&t1.log): not reported
```

A store of another pointer where the pointer is loaded from, between the
load and the read, does not stop a path: the pointer read is the one loaded.

```go
p := newMonitor()
p.log = zerolog.Ctx(ctx).With().Logger() // the only store that reaches the read
p.log.Info().Msg("x")                    // not reported
p.log = logger                           // after the read
```

A global can be changed anywhere, by another goroutine too, so what a load of
it read is not known either. An update of a global, which builds on such a
load, is left out, and the path goes on past it:

```go
globalHolder.log = globalHolder.log.With().Ctx(ctx).Logger() // left out
globalHolder.log.Info().Msg("x")                             // reported
```

#### Address Matching

Addresses are compared structurally, not by identity: SSA emits a fresh
`FieldAddr`/`IndexAddr` for every access, so the write and read halves of a
nested access (`h.inner.event`) never share a value. `addressesMatch`
compares each selection with `sel.same`, from the outermost in, and takes an
instruction for the same as itself. So an element at a variable index matches
only through the same `IndexAddr` instruction. The walk of stores does not use
it for an element; it uses `Place.covers`. Under the selections, two
loads of the same address, or two fields of the same struct value, match
when the same stores reach them.

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

When the walk back from a read meets a store of the whole root, or of a
place the path starts with, `storeWalk.copied` follows the copy back to the
temporary, and walks on from the load of it with the rest of the path.
The copy is followed the same way when the root is a pointer that is not in a
variable, and so is a copy from one. Both shapes are handled, since whether
the compiler emits the temporary depends on the x/tools version.

A copy from a variable that a closure captures is followed too. Each walk
that reaches the entry of the closure adds its variable to the result, so
both variables are traced here:

```go
func() {
    if cond {
        g = h          // a copy of h, which the closure captures
    }
    g.l.Info().Msg("x") // reads h.l or g.l: both must be traced
}()
```

### The Library Model

`ssaflow` knows SSA and `go/types`, not zerolog. Two facts about zerolog
change what a read sees, and the signatures do not tell them. `ssaflow.Model`
asks for them, and `zerologapi.Model` answers:

| Method | Asked by | zerolog's answer |
|--------|----------|------------------|
| `IsHandle(ptr)` | `Resolver.ResolvesThrough`: does a read go through this pointer to what is stored there? | `*zerolog.Event`: an event is the value itself, not a variable it is read from |
| `LeavesReceiver(fn)` | `storeReadsOf`, for every read of an address: is a call on it a read? | `KindInPlaceUpdate`, as `Logger.UpdateContext` is: it changes the fields of the Logger but not its context |

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

An ignore reaches the line it is on. An ignore on a line of its own also reaches the line below; one at the end of a line of code does not. A report sits at the `(` of the reported call, so a chain that spans several lines is also reached from the line where its call expression starts. So an ignore at the end of the first line of a chain reaches the whole chain. `directive.Scan` records that start for every call, and whether each ignore follows code on its line, from the file's AST.

None of these reported comments silences anything (#60). Guessing what was meant would silence a report nobody asked to silence.

## Known Limitations

This is the one list of known limitations. Each row has a `// LIMITATION`
fixture under `testdata/src/zerolog/`, tagged `// LIMITATION(#n)` where an
issue tracks the case.

A false positive is a report on code that passes the context. A false
negative is a missed report.

### False positives

| Limitation | What happens | Fixture |
|------------|--------------|---------|
| Helper function returns | An event or Logger a declared function returns is not traced into it. An IIFE and its arguments are | `iife` |
| Channels | An event received from a channel is not traced to what was sent | `memory` |
| `sync.Pool` | An event from `Pool.Get` is not traced to the `New` function | `memory` |
| Slice and map elements | An event read back from a slice or a map is not traced. One stored with `.Ctx(ctx)` is still reported | `memory` |
| Elements at an index computed in a loop | A store at the same index, on the same iteration, is not taken to write the element. A store without `.Ctx(ctx)` on an earlier iteration may still be what the read sees. So may the caller's value in a slice parameter | `memory` |
| Stores by a closure | A store by a closure to a variable it captures is not seen by a read after the closure is called | `closures` |
| In-place `Ctx` through another reference (#139) | `e.Ctx(ctx)` with its result unused is not seen by a terminator in a closure that captures `e`. Nor is it seen in a func literal `e` is passed to | `inplace` |
| Pointers picked on many branches | A pointer that a Phi picks out of more than 16 (`storePickedMax`) is not followed | `memory` |
| A method value assigned twice, in a closure | A closure that loads a method value from a variable assigned more than once does not know which method it calls. Nor does one that loads a method value another closure stored | `methods` |
| A pointer read in a closure that is not only called | A pointer that a closure reads from a captured variable, such as `h.m` or `*pp`, is followed out of the closure only when the closure is only called where it is made. One run by a `go` or `defer` statement, or passed on, does not see a store through the pointer before it | `closures` |
| A captured pointer picked on a branch | A pointer that a closure captures, picked on a branch, is not followed. The walk from where the closure runs does not keep to the paths of the Phi | `closures` |
| A struct loaded through a pointer a call returns | The struct is not followed, so each read of the event in it is an event of its own | `phi` |
| `Context.CreateDict` | Tracing does not follow the dict back to the Context. A dict that carries the context from `Context.Ctx` is taken to have none | `logger` |
| An IIFE with an unreachable return | Every return of the func literal is checked, even one that cannot run | `iife` |

### False negatives

| Limitation | What happens | Fixture |
|------------|--------------|---------|
| Events logged in another function | An event passed to a method or function that logs it is not checked there | `memory` |
| A Logger used as `io.Writer` | A call through `io.Writer`, such as `fmt.Fprint(l, "x")`, is not checked | `directlog` |
| Events made in a loop | A value can hold the event of the latest run of a call on one path, and of an earlier run on another. A `Ctx` call on it counts for the latest run on both paths | `inplace` |
| Events stored back | A `Ctx` call on a read that can see several events counts for each of them. It counts even where a path stores one of those events back over another after the call | `inplace` |
| The caller's event copied over | A `Ctx` call on the caller's event at a place counts at later reads of the place. It counts even when another value of the caller's is copied over it in between | `inplace` |
| Panics | A deferred call is checked at the returns after its `defer` statement. It is not checked at the points where a panic could run it | `deferred` |
| Writes through another reference (#115) | A write through a pointer passed to a function is not seen. Nor is one through another slice of the same array, or another pointer parameter. The value stored before it is taken as the one read | `memory` |
| Fresh contexts in a func literal | A fresh context given to `Ctx` in a func literal called in place does not replace the context the event had | `freshctx` |
| Fresh contexts on another run | A fresh `Ctx` call on an event made in a loop counts only for a value that holds the latest run. It is not seen for the event kept from the last iteration | `freshctx` |
| A captured pointer to a field | A pointer to a field of another variable, captured by a closure, may point anywhere. What it points to is not taken as fresh | `freshctx` |

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

# zerologlintctx

[![Go Reference](https://pkg.go.dev/badge/github.com/mpyw/zerologlintctx.svg)](https://pkg.go.dev/github.com/mpyw/zerologlintctx)
[![CI](https://github.com/mpyw/zerologlintctx/actions/workflows/ci.yml/badge.svg)](https://github.com/mpyw/zerologlintctx/actions/workflows/ci.yml)
[![Codecov](https://codecov.io/gh/mpyw/zerologlintctx/graph/badge.svg)](https://codecov.io/gh/mpyw/zerologlintctx)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go linter that reports a [zerolog](https://pkg.go.dev/github.com/rs/zerolog) chain that does not pass on the available context.

## Overview

```go
package app

import (
	"context"

	"github.com/rs/zerolog"
)

func Place(
	ctx context.Context,
	log zerolog.Logger,
	id string,
) {
	log.Info().
		Str("order", id).
		Msg("order placed")
}
```

```console
$ zerologlintctx ./...
order.go:16:6: zerolog call chain missing .Ctx(ctx)
```

The function has a [`context.Context`](https://pkg.go.dev/context#Context). The event does not get it, because the chain has no [`.Ctx(ctx)`](https://pkg.go.dev/github.com/rs/zerolog#Event.Ctx). A [hook](https://pkg.go.dev/github.com/rs/zerolog#Hook) reads the context of the event with [`Event.GetCtx`](https://pkg.go.dev/github.com/rs/zerolog#Event.GetCtx):

<table>
<thead>
<tr><th>Code</th><th>What the hook sees</th></tr>
</thead>
<tbody>
<tr>
<td>

```go
log.Info().
	Str("order", id).
	Msg("order placed")
```

</td>
<td><b>No context.</b> <code>GetCtx</code> returns <code>context.Background()</code>. A trace ID in <code>ctx</code> is not in the log line</td>
</tr>
<tr>
<td>

```go
log.Info().
	Ctx(ctx).
	Str("order", id).
	Msg("order placed")
```

</td>
<td><b>The caller's context.</b> <code>GetCtx</code> returns <code>ctx</code></td>
</tr>
</tbody>
</table>

The code compiles, and the line is still logged. zerologlintctx finds it from the source.

## Install

| Method | Command | Needs |
| --- | --- | --- |
| **[mise](https://mise.jdx.dev/)** *(recommended)* | `mise use "github:mpyw/zerologlintctx@0.14.1"` | Nothing. Installs the prebuilt binary |
| [`go tool`](https://pkg.go.dev/cmd/go#hdr-Run_specified_go_tool) | `go get -tool github.com/mpyw/zerologlintctx/cmd/zerologlintctx@latest` | Go 1.27+ |
| [`go install`](https://pkg.go.dev/cmd/go#hdr-Compile_and_install_packages_and_dependencies) | `go install github.com/mpyw/zerologlintctx/cmd/zerologlintctx@latest` | Go 1.27+ |
| [`go run`](https://pkg.go.dev/cmd/go#hdr-Compile_and_run_Go_program) | `go run github.com/mpyw/zerologlintctx/cmd/zerologlintctx@latest ./...` | Go 1.27+ |
| Release archive | See below | Nothing |

```bash
zerologlintctx ./...
```

### Requirements

Building from source needs Go 1.27 or later. The analyzed code may target any Go version.

> [!CAUTION]
> To prevent supply chain attacks, pin a version tag instead of `@latest` in CI/CD pipelines, such as `@v0.14.1`.

<details>
<summary>Pin a version, run through <code>go vet</code> or golangci-lint, or install from an archive</summary>

`mise use` pins the version in the project's `mise.toml`, so every checkout and CI run the same one. Run it in the project root. Add `-g` to install it for every project on your machine instead.

```toml
[tools]
"github:mpyw/zerologlintctx" = "0.14.1"
```

As a tool dependency in `go.mod`:

```bash
go get -tool github.com/mpyw/zerologlintctx/cmd/zerologlintctx@latest
go tool zerologlintctx ./...
```

Through [`go vet`](https://pkg.go.dev/cmd/go#hdr-Report_likely_mistakes_in_packages). zerologlintctx has no flags of its own, so `go vet` can run it:

```bash
go install github.com/mpyw/zerologlintctx/cmd/zerologlintctx@latest
go vet -vettool=$(which zerologlintctx) ./...
```

From a release archive on [GitHub Releases](https://github.com/mpyw/zerologlintctx/releases), verified against the published checksums:

```bash
export VERSION=0.14.1
export OS=linux    # or darwin
export ARCH=amd64  # or arm64
export BASE_URL="https://github.com/mpyw/zerologlintctx/releases/download/v${VERSION}"

# Download the archive and the release's checksum list
curl -LO "${BASE_URL}/zerologlintctx_${VERSION}_${OS}_${ARCH}.tar.gz"
curl -LO "${BASE_URL}/checksums.txt"

# Verify before installing (use `shasum -a 256 -c` on macOS)
sha256sum --ignore-missing -c checksums.txt

tar xzf "zerologlintctx_${VERSION}_${OS}_${ARCH}.tar.gz"
sudo mv zerologlintctx /usr/local/bin/
```

On Windows, download `zerologlintctx_${VERSION}_windows_${ARCH}.zip` and extract `zerologlintctx.exe` somewhere on your `PATH`.

Inside golangci-lint, as a [module plugin](https://golangci-lint.run/plugins/module-plugins/). zerologlintctx is not bundled with golangci-lint, so build a binary that holds it. Write `.custom-gcl.yml`:

```yaml
version: v2.15.0  # the golangci-lint release to build
plugins:
  - module: github.com/mpyw/zerologlintctx
    import: github.com/mpyw/zerologlintctx/plugin
    version: v0.14.1
```

Turn it on in `.golangci.yml`. It takes no settings, and any key under `settings` stops the run. To skip test files, use golangci-lint's `run.tests: false` instead of `-test=false`:

```yaml
version: "2"
linters:
  enable:
    - zerologlintctx
  settings:
    custom:
      zerologlintctx:
        type: module
        description: Checks that zerolog chains carry the context.
```

Then build and run it:

```bash
golangci-lint custom  # writes ./custom-gcl
./custom-gcl run ./...
```

</details>

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-test` | `true` | Analyze test files (`*_test.go`). This is a flag of the built-in driver |

```bash
# Exclude test files from analysis
zerologlintctx -test=false ./...
```

A generated file is never analyzed, and no flag turns it on:

```go
// Code generated by mockgen. DO NOT EDIT.

package app

func Place(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("order placed") // ✅️ not reported
}
```

## What it checks

A function is checked when it has a `context.Context` parameter. The report names that parameter, such as `.Ctx(ctx)` or `.Ctx(c)`.

The examples below are bodies of this function:

```go
func handler(
	ctx context.Context,
	log zerolog.Logger,
	verbose bool,
	failed bool,
) {
	// example
}
```

### Event chains

Every chain that sends an event, with `Msg`, `Msgf`, `MsgFunc` or `Send`, must carry the context:

<table>
<thead>
<tr><th>Code</th><th>Verdict</th></tr>
</thead>
<tbody>
<tr>
<td>

```go
log.Info().
	Str("key", "value").
	Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> The chain has no <code>.Ctx(ctx)</code></td>
</tr>
<tr>
<td>

```go
log.Info().
	Ctx(ctx).
	Str("key", "value").
	Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> The event carries <code>ctx</code></td>
</tr>
<tr>
<td>

```go
l := log.With().
	Ctx(ctx).
	Logger()
l.Info().Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> The logger carries <code>ctx</code>, and its events take it</td>
</tr>
<tr>
<td>

```go
zerolog.Ctx(ctx).
	Info().
	Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> The logger is taken from <code>ctx</code>. <code>log.Ctx(ctx)</code> of <code>zerolog/log</code> is the same</td>
</tr>
<tr>
<td>

```go
e := log.Info()
e = e.Ctx(ctx)
e.Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> A variable is followed to the chain it holds</td>
</tr>
<tr>
<td>

```go
e := log.Info()
if verbose {
	e = e.Ctx(ctx)
}
e.Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> On the path where <code>verbose</code> is false, the event has no context</td>
</tr>
<tr>
<td>

```go
const debug = true

e := log.Info()
if debug {
	e = e.Ctx(ctx)
}
e.Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> The condition is a constant, so the branch always runs. A path that skips it never runs</td>
</tr>
<tr>
<td>

```go
const debug = false

e := log.Info()
if debug {
	e = e.Ctx(ctx)
}
e.Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> The branch never runs, so the event has no context</td>
</tr>
<tr>
<td>

```go
const mode = modeOn

e := log.Info()
switch mode {
case modeOn:
	e = e.Ctx(ctx)
}
e.Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> The tag and the case are constants, so the case always matches</td>
</tr>
<tr>
<td>

```go
e := log.Info()
for range 1 {
	e.Ctx(ctx)
}
e.Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> The first check of the loop compares two constants, so the loop always runs once</td>
</tr>
<tr>
<td>

```go
e := log.Info().Ctx(ctx)
func() {
	e.Msg("hello")
}()
```

</td>
<td>✅️ <b>Passes.</b> A closure is followed to the variable it captures</td>
</tr>
<tr>
<td>

```go
l := log
h := box{p: &l}
*h.p = log.With().Ctx(ctx).Logger()
l.Info().Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> <code>h.p</code> holds <code>&l</code>, so the store through it writes <code>l</code></td>
</tr>
<tr>
<td>

```go
defer log.Info().Msg("done")
go log.Info().Msg("started")
```

</td>
<td>❌️ <b>Reported.</b> Both lines. A <code>go</code> or <code>defer</code> statement is checked as a plain call</td>
</tr>
</tbody>
</table>

> [!NOTE]
> [`zerolog.Ctx(ctx)`](https://pkg.go.dev/github.com/rs/zerolog#Ctx) returns the logger stored in `ctx`. It does not attach `ctx` to the events of that logger. Still, zerologlintctx accepts it, because taking the logger from the context is a deliberate use of it. Add `.Ctx(ctx)` too if hooks need the context:
>
> ```go
> zerolog.Ctx(ctx).
> 	Info().
> 	Ctx(ctx).
> 	Msg("hello")
> ```

A function without a context has nothing to pass on, so it is not checked:

```go
func worker(log zerolog.Logger) {
	log.Info().Msg("hello") // ✅️ not reported
}
```

### Calls that do not pass on the context

Some calls look like they pass on the context, but do not. They do not count:

<table>
<thead>
<tr><th>Code</th><th>Verdict</th></tr>
</thead>
<tbody>
<tr>
<td>

```go
log.Info().
	Ctx(context.Background()).
	Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> A fresh context carries nothing of the caller's</td>
</tr>
<tr>
<td>

```go
zerolog.Ctx(context.TODO()).
	Info().
	Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> The same, for the logger stored in a context</td>
</tr>
<tr>
<td>

```go
bg := context.Background()
log.Info().
	Ctx(bg).
	Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> A local variable that only holds fresh contexts is fresh too</td>
</tr>
<tr>
<td>

```go
l := log.With().
	Ctx(ctx).
	Logger().
	Output(os.Stderr)
l.Info().Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> <a href="https://pkg.go.dev/github.com/rs/zerolog#Logger.Output"><code>Logger.Output</code></a> builds a new logger, and drops the context</td>
</tr>
<tr>
<td>

```go
l := zerolog.Ctx(ctx).
	Output(os.Stderr)
l.Info().Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> A logger from <code>zerolog.Ctx(ctx)</code> still counts after <code>Output</code></td>
</tr>
<tr>
<td>

```go
e := log.Info()
d := e.CreateDict().
	Ctx(ctx).
	Str("k", "v")
e.Dict("d", d).Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> <a href="https://pkg.go.dev/github.com/rs/zerolog#Event.CreateDict"><code>Event.CreateDict</code></a> returns a new event. The context is set on the dict, not on <code>e</code></td>
</tr>
</tbody>
</table>

### Direct logging

`Print`, `Printf`, `Println` and `Write` log without an event chain, so they take no `.Ctx(ctx)`. The event gets only the logger's own context. A call is reported when the logger does not carry the context:

<table>
<thead>
<tr><th>Code</th><th>Verdict</th></tr>
</thead>
<tbody>
<tr>
<td>

```go
log.Print("hello")
log.Printf("hello %s", "world")
```

</td>
<td>❌️ <b>Reported.</b> Both lines. The logger has no context</td>
</tr>
<tr>
<td>

```go
l := log.With().
	Ctx(ctx).
	Logger()
l.Print("hello")
```

</td>
<td>✅️ <b>Passes.</b> The logger carries the context</td>
</tr>
<tr>
<td>

```go
log.Write([]byte("hello"))
```

</td>
<td>❌️ <b>Reported.</b> <code>Write</code> is the method that makes a <code>Logger</code> an <code>io.Writer</code>. A call of it is checked as <code>Print</code> is</td>
</tr>
<tr>
<td>

```go
fmt.Fprint(log, "hello")
```

</td>
<td>⚠️ <b>Not reported (limitation).</b> A use through <code>io.Writer</code> is not checked</td>
</tr>
</tbody>
</table>

The `Print` functions of `github.com/rs/zerolog/log` use the global logger, which is not traced. They are always reported:

```go
import "github.com/rs/zerolog/log"

func handler(ctx context.Context) {
	log.Print("hello") // ❌️ reported
}
```

## Ignoring a report

Write `//zerologlintctx:ignore`, with a reason after `//`:

```go
//zerologlintctx:ignore // not tied to a request
log.Info().Msg("background task")
```

<table>
<thead>
<tr><th>Placement</th><th>Verdict</th></tr>
</thead>
<tbody>
<tr>
<td>

```go
//zerologlintctx:ignore // reason
log.Info().Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> An ignore on a line of its own reaches the line below</td>
</tr>
<tr>
<td>

```go
//zerologlintctx:ignore // reason
log.Info().
	Str("k", "v").
	Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> A chain that starts on that line is reached, even when it spans several lines</td>
</tr>
<tr>
<td>

```go
log.Info().Msg("hello") //zerologlintctx:ignore
```

</td>
<td>✅️ <b>Passes.</b> An ignore at the end of a line of code reaches that line</td>
</tr>
<tr>
<td>

```go
log.Info(). //zerologlintctx:ignore
	Str("k", "v").
	Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> A chain that starts on that line is reached, even when it spans several lines</td>
</tr>
<tr>
<td>

```go
log.Info().Msg("a") //zerologlintctx:ignore
log.Info().Msg("b")
```

</td>
<td>❌️ <b>Reported.</b> Only <code>b</code>. An ignore at the end of a line does not reach the line below</td>
</tr>
<tr>
<td>

```go
//zerologlintctx:ignore - reason
log.Info().Msg("hello")
```

</td>
<td>✅️ <b>Passes.</b> A reason after <code> - </code> also works, for compatibility</td>
</tr>
</tbody>
</table>

A directive that is not written right is reported, and silences nothing:

<table>
<thead>
<tr><th>Directive</th><th>Verdict</th></tr>
</thead>
<tbody>
<tr>
<td>

```go
//zerologlintctx:bogus
log.Info().Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> Two reports: <code>unknown directive zerologlintctx:bogus</code>, and the chain</td>
</tr>
<tr>
<td>

```go
// zerologlintctx:ignore
log.Info().Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> Two reports: a space after <code>//</code> makes the directive malformed, and the chain is reported too</td>
</tr>
<tr>
<td>

```go
//zerologlintctx:ignore not tied to a request
log.Info().Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> Two reports: <code>ignore</code> takes no argument, and the chain is reported too. Write the reason after <code>//</code></td>
</tr>
<tr>
<td>

```go
//zerologlintctx:ignore // reason
log.Info().Ctx(ctx).Msg("hello")
```

</td>
<td>❌️ <b>Reported.</b> <code>unused zerologlintctx:ignore directive</code>. The ignore suppresses nothing</td>
</tr>
</tbody>
</table>

## Limits

The analyzer uses [SSA](https://pkg.go.dev/golang.org/x/tools/go/ssa) (Static Single Assignment) form to follow events through variables, branches and closures. Some value flows are not followed.

These are reported, although the context is passed:

<table>
<thead>
<tr><th>Code</th><th>Verdict</th></tr>
</thead>
<tbody>
<tr>
<td>

```go
e := newEvent(ctx, log)
e.Msg("hello")

// elsewhere
func newEvent(
	ctx context.Context,
	log zerolog.Logger,
) *zerolog.Event {
	return log.Info().Ctx(ctx)
}
```

</td>
<td>⚠️ <b>Reported (limitation).</b> An event that a declared function returns is not traced into it</td>
</tr>
<tr>
<td>

```go
ch := make(chan *zerolog.Event, 1)
ch <- log.Info().Ctx(ctx)
e := <-ch
e.Msg("hello")
```

</td>
<td>⚠️ <b>Reported (limitation).</b> An event received from a channel is not traced to what was sent</td>
</tr>
<tr>
<td>

```go
ch := make(chan zerolog.Logger, 1)
ch <- log.With().Ctx(ctx).Logger()
l := <-ch
l.Info().Msg("hello")
```

</td>
<td>⚠️ <b>Reported (limitation).</b> The same for a Logger received from a channel</td>
</tr>
<tr>
<td>

```go
var e *zerolog.Event
f := func() {
	e = log.Info().Ctx(ctx)
}
f()
e.Msg("hello")
```

</td>
<td>⚠️ <b>Reported (limitation).</b> A store by a closure to a variable it captures is not seen after the call</td>
</tr>
<tr>
<td>

```go
l := log
setCtx(ctx, &l, log)
l.Info().Msg("hello")

// elsewhere
func setCtx(
	ctx context.Context,
	p *zerolog.Logger,
	log zerolog.Logger,
) {
	*p = log.With().Ctx(ctx).Logger()
}
```

</td>
<td>⚠️ <b>Reported (limitation).</b> A store by a function that is passed the address is not seen. The read sees <code>l</code> as it was before the call</td>
</tr>
<tr>
<td>

```go
for range n {
	p := newLogger()
	defer p.Print("hello")
	*p = log.With().Ctx(ctx).Logger()
}

// elsewhere
func newLogger() *zerolog.Logger {
	return new(zerolog.Logger)
}
```

</td>
<td>⚠️ <b>Reported (limitation).</b> The pointer is made again on each iteration. So a store through it after the <code>defer</code> may be through a later pointer, and the value before the <code>defer</code> is still seen</td>
</tr>
</tbody>
</table>

These are not reported, although the context is not passed:

<table>
<thead>
<tr><th>Code</th><th>Verdict</th></tr>
</thead>
<tbody>
<tr>
<td>

```go
send(log.Info())

// elsewhere
func send(e *zerolog.Event) {
	e.Msg("hello")
}
```

</td>
<td>⚠️ <b>Not reported (limitation).</b> An event passed to a function that logs it is not checked there. <code>send</code> has no context of its own</td>
</tr>
<tr>
<td>

```go
var w interface{ Msg(string) } = log.Info()
w.Msg("hello")
```

</td>
<td>⚠️ <b>Not reported (limitation).</b> A call through an interface the event is assigned to is not checked</td>
</tr>
<tr>
<td>

```go
l := log.With().Ctx(ctx).Logger()
func() {
	l = log
}()
l.Info().Msg("hello")
```

</td>
<td>⚠️ <b>Not reported (limitation).</b> A store by a closure is not seen after the call here either. The read sees <code>l</code> as it was before the call, with the context</td>
</tr>
<tr>
<td>

```go
l := log.With().Ctx(ctx).Logger()
p := &l
func() {
	*p = log
}()
p.Info().Msg("hello")
```

</td>
<td>⚠️ <b>Not reported (limitation).</b> The same for a store through a pointer the closure captures</td>
</tr>
<tr>
<td>

```go
e := log.Info()
defer e.Msg("done")
if failed {
	panic("failed")
}
e.Ctx(ctx)
```

</td>
<td>⚠️ <b>Not reported (limitation).</b> A deferred call is checked at the returns after its <code>defer</code> statement, not where a panic could run it</td>
</tr>
</tbody>
</table>

The full list, with the fixture of each, is in [Known limitations](./docs/ARCHITECTURE.md#known-limitations).

## Design principles

| Principle | Means |
| --- | --- |
| **No false positives, as a goal** | Prefer missing issues over false alarms. The [known limitations](./docs/ARCHITECTURE.md#known-limitations) list the false positives that remain |
| **Type-safe analysis** | Uses [`go/types`](https://pkg.go.dev/go/types) for accurate detection |
| **SSA-based tracking** | Uses [SSA](https://pkg.go.dev/golang.org/x/tools/go/ssa) form to track events through assignments and closures |
| **Nested functions** | Tracks the context through closures |

## Documentation

- [Architecture](./docs/ARCHITECTURE.md): internal design and detection logic
- [AGENTS.md](./AGENTS.md): repository instructions for contributors and coding agents

## Related tools

| Tool | Checks |
| --- | --- |
| [zerologlint](https://github.com/ykadowak/zerologlint) | General zerolog best practices |
| [contextcheck](https://github.com/kkHAIKE/contextcheck) | A new context, with [`context.Background()`](https://pkg.go.dev/context#Background) or [`context.TODO()`](https://pkg.go.dev/context#TODO), where one should be passed on. A missing context parameter |
| zerologlintctx | An available context that a zerolog chain does not use |

## AI usage

zerologlintctx is developed with AI coding agents (Claude Code). The fixtures under `testdata/src/` define what it reports.

## License

MIT

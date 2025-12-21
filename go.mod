module github.com/mpyw/zerologlintctx

go 1.24.0

require golang.org/x/tools v0.40.0

require (
	golang.org/x/mod v0.31.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
)

// Retract all previous versions due to critical bugs:
// - v0.0.1 to v0.3.0: -test flag conflicted with singlechecker's built-in flag
// - v0.4.0: False positives for log.Ctx(ctx) patterns, missing direct logging detection
// - v0.5.0 to v0.6.1: Package name was `analyzer` instead of `zerologlintctx`
retract [v0.0.1, v0.6.1]

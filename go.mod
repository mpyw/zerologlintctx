module github.com/mpyw/zerologlintctx

go 1.24.0

require golang.org/x/tools v0.40.0

require (
	golang.org/x/mod v0.31.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
)

// Retract all previous versions due to -test flag not working
// (conflicted with singlechecker's built-in flag)
retract [v0.0.1, v0.3.0]

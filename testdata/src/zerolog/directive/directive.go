// Package directive covers //zerologlintctx:ignore directives and the problems
// reported on them.
package directive

import (
	"context"

	"github.com/rs/zerolog"
)

// ===== IGNORE COMMENTS =====

func goodIgnoredSameLine(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("ignored") //zerologlintctx:ignore
}

func goodIgnoredPreviousLine(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore
	log.Info().Msg("ignored")
}

// An ignore above a chain that spans several lines reaches the whole chain (#76).
func goodIgnoredAboveMultiLineChain(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore
	log.Info().
		Str("k", "v").
		Msg("ignored")
}

func goodIgnoredInsideMultiLineChain(ctx context.Context, log zerolog.Logger) {
	log.Info().
		Str("k", "v").
		//zerologlintctx:ignore
		Msg("ignored")
}

func goodIgnoredAboveMultiLineDefer(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore
	defer log.Info().
		Str("k", "v").
		Msg("ignored")
}

// A go or defer call is reported at the "(" of the call, like any other, so a
// trailing ignore on that line reaches it (#100).
func goodTrailingIgnoreOnMultiLineDefer(ctx context.Context, log zerolog.Logger) {
	defer log.Info().
		Str("k", "v").
		Msg("ignored") //zerologlintctx:ignore
}

func goodTrailingIgnoreOnMultiLineGo(ctx context.Context, log zerolog.Logger) {
	go log.Info().
		Str("k", "v").
		Msg("ignored") //zerologlintctx:ignore
}

func badMultiLineDeferReportedAtCall(ctx context.Context, log zerolog.Logger) {
	defer log.Info().
		Str("k", "v").
		Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// An ignore above a call does not reach the calls inside a closure passed to it.
func badIgnoreAboveCallDoesNotReachClosure(ctx context.Context, log zerolog.Logger, run func(func())) {
	//zerologlintctx:ignore // want `unused zerologlintctx:ignore directive`
	run(func() {
		log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
	})
}

// A trailing ignore reaches its own line only, not the line below (#77).
func badTrailingIgnoreDoesNotReachNextLine(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("ignored")     //zerologlintctx:ignore
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badTrailingIgnoreOnBlockOpener(ctx context.Context, log zerolog.Logger, cond bool) {
	if cond { //zerologlintctx:ignore // want `unused zerologlintctx:ignore directive`
		log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
	}
}

// A space after "//" makes it malformed, and it suppresses nothing.
func badIgnoreWithSpaceSameLine(ctx context.Context, log zerolog.Logger) {
	log.Info().Msg("not ignored") // zerologlintctx:ignore // want `zerolog call chain missing .Ctx\(ctx\)` `malformed zerologlintctx directive: write it as //zerologlintctx:name`
}

func goodIgnoredWithReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore - intentionally not passing context
	log.Info().Msg("ignored")
}

func badIgnoreWithSpacePreviousLine(ctx context.Context, log zerolog.Logger) {
	// zerologlintctx:ignore // want `malformed zerologlintctx directive: write it as //zerologlintctx:name`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A directive name that only starts with "ignore" is not the ignore directive.
func badIgnoreLookalikeName(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignored // want `unknown directive zerologlintctx:ignored`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A trailing comment is a reason, with or without a space before it.
func goodIgnoredWithSlashReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore // intentionally not passing context
	log.Info().Msg("ignored")
}

func goodIgnoredWithSlashReasonNoSpace(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore //intentionally not passing context
	log.Info().Msg("ignored")
}

func goodIgnoredWithGluedSlashReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore//intentionally not passing context
	log.Info().Msg("ignored")
}

func goodIgnoredWithDashAndSlashReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore - intentionally // not passing context
	log.Info().Msg("ignored")
}

// The ignore takes no argument. Text that is not after "//" or " - " is
// reported, and the ignore suppresses nothing.
func badIgnoreWithFreeText(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore intentionally detached // want `zerologlintctx:ignore takes no argument; write a reason after //`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A misspelled name is an unknown directive, and suppresses nothing.
func badIgnoreMisspelled(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignre // want `unknown directive zerologlintctx:ignre`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A reason glued on with "-" makes the name unknown.
func badIgnoreGluedDashReason(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore-reason // want `unknown directive zerologlintctx:ignore-reason`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// A tool name that only starts with "zerologlintctx" is not this tool.
func badIgnoreLookalikeTool(ctx context.Context, log zerolog.Logger) {
	//zerologlintctxx:ignore
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// ===== MALFORMED DIRECTIVES =====

func badMalformedSpaceAfterColon(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx: ignore // want `malformed zerologlintctx directive: write it as //zerologlintctx:name`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

func badMalformedBlockComment(ctx context.Context, log zerolog.Logger) {
	/*zerologlintctx:ignore*/ // want `malformed zerologlintctx directive: write it as //zerologlintctx:name`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Directive names are lowercase.
func badMalformedUppercaseName(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:Ignore // want `malformed zerologlintctx directive: write it as //zerologlintctx:name`
	log.Info().Msg("not ignored") // want `zerolog call chain missing .Ctx\(ctx\)`
}

// Prose that mentions zerologlintctx:ignore in the middle of a comment is not
// a directive, so it is not reported as malformed.
func goodProseMentioningDirective(ctx context.Context, log zerolog.Logger) {
	// Use zerologlintctx:ignore to suppress a report.
	log.Info().Ctx(ctx).Msg("ok")
}

// ===== UNUSED IGNORE DIRECTIVES =====

func badUnusedIgnore(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore  // want `unused zerologlintctx:ignore directive`
	log.Info().Ctx(ctx).Msg("already has ctx, ignore not needed")
}

func badUnusedIgnoreSameLine(ctx context.Context, log zerolog.Logger) {
	log.Info().Ctx(ctx).Msg("already has ctx") //zerologlintctx:ignore  // want `unused zerologlintctx:ignore directive`
}

func badUnusedIgnoreNoLog(ctx context.Context, log zerolog.Logger) {
	//zerologlintctx:ignore  // want `unused zerologlintctx:ignore directive`
	_ = ctx
}

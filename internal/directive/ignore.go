// Package directive provides handling of zerologlintctx directive comments.
//
// # Supported Directives
//
// The package recognizes the following comment directive:
//
//	//zerologlintctx:ignore
//
// It follows the Go directive syntax: no space after "//" or after the colon.
// It takes no argument. A reason goes in a trailing comment after "//", or
// after " - ", which is kept for compatibility:
//
//	//zerologlintctx:ignore // intentionally not passing context
//	//zerologlintctx:ignore - intentionally not passing context
//
// This directive can be placed on the same line or the line before the code
// to suppress warnings.
//
// # Usage Examples
//
// Same-line ignore:
//
//	log.Info().Msg("no ctx needed") //zerologlintctx:ignore
//
// Previous-line ignore:
//
//	//zerologlintctx:ignore
//	log.Info().Msg("no ctx needed")
//
// Unused ignore directives are reported as errors to keep the codebase clean.
// Any other comment that starts with "zerologlintctx:", such as
// "// zerologlintctx:ignore", is reported as malformed. A directive with
// another name, such as "//zerologlintctx:ignre", is reported as unknown, and
// an ignore with an argument is reported too. None of them silences anything.
package directive

import (
	"go/ast"
	"go/token"
	"strings"
	"unicode"
)

// directiveTool is the tool part of every zerologlintctx directive.
const directiveTool = "zerologlintctx"

// ignoreEntry tracks an ignore directive and whether it was used.
type ignoreEntry struct {
	pos  token.Pos // Position of the ignore comment
	used bool      // Whether this ignore was actually used to suppress a warning
}

// IgnoreMap tracks line numbers that have ignore comments.
type IgnoreMap map[int]*ignoreEntry

// BuildIgnoreMap scans a file for ignore comments and returns a map.
func BuildIgnoreMap(fset *token.FileSet, file *ast.File) IgnoreMap {
	m := make(IgnoreMap)
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if isIgnoreComment(c.Text) {
				line := fset.PositionFor(c.Pos(), false).Line
				m[line] = &ignoreEntry{pos: c.Pos(), used: false}
			}
		}
	}
	return m
}

// isIgnoreComment checks if a comment is an ignore directive.
// Only the canonical form "//zerologlintctx:ignore" counts, with no space
// after "//". A reason may follow after "//" or " - "; other text may not.
func isIgnoreComment(text string) bool {
	d, ok := parse(text)
	return ok && d.Name == "ignore" && isReason(d.Args)
}

// isReason reports whether args, the text after an ignore directive, is empty
// or a reason after " - ". The " - " form predates "//" and is kept so that
// existing ignores keep working.
func isReason(args string) bool {
	return args == "" || args == "-" || strings.HasPrefix(args, "- ")
}

// parse parses a comment as a zerologlintctx directive. It reports false for
// any comment that is not the canonical form "//zerologlintctx:name [args]",
// including another tool's directive.
//
// A trailing comment explains the directive and is dropped first:
// "//zerologlintctx:ignore // reason" and "//zerologlintctx:ignore//reason"
// are both a bare ignore.
func parse(text string) (ast.Directive, bool) {
	if body, ok := strings.CutPrefix(text, "//"); ok {
		if i := strings.Index(body, "//"); i >= 0 {
			text = "//" + body[:i]
		}
	}
	d, ok := ast.ParseDirective(token.NoPos, text)
	if !ok || d.Tool != directiveTool {
		return ast.Directive{}, false
	}
	return d, true
}

// Problem is a comment addressed to zerologlintctx that does nothing.
type Problem struct {
	Pos     token.Pos
	Message string
}

// FindProblems returns the comments that are addressed to zerologlintctx but
// do nothing: malformed directives, unknown directives, and ignore directives
// with text that is not a reason.
func FindProblems(file *ast.File) []Problem {
	var found []Problem
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if msg := problem(c.Text); msg != "" {
				found = append(found, Problem{Pos: c.Pos(), Message: msg})
			}
		}
	}
	return found
}

// problem returns why text, a comment addressed to zerologlintctx, does
// nothing, or "" when it is a working directive or not addressed at all.
//
// A comment is addressed when its body, after "//" or "/*", starts with
// "zerologlintctx:" once leading whitespace is skipped. Prose that mentions
// zerologlintctx elsewhere in a comment is not addressed.
func problem(text string) string {
	d, ok := parse(text)
	switch {
	case !ok && isAddressed(text):
		return "malformed zerologlintctx directive: write it as //zerologlintctx:name"
	case !ok:
		return ""
	case d.Name != "ignore":
		return "unknown directive zerologlintctx:" + d.Name
	case !isReason(d.Args):
		return "zerologlintctx:ignore takes no argument; write a reason after //"
	}
	return ""
}

// isAddressed reports whether text starts with "zerologlintctx:" after the
// comment marker and any whitespace.
func isAddressed(text string) bool {
	body, ok := strings.CutPrefix(text, "//")
	if !ok {
		body, ok = strings.CutPrefix(text, "/*")
	}
	return ok && strings.HasPrefix(strings.TrimLeftFunc(body, unicode.IsSpace), directiveTool+":")
}

// ShouldIgnore returns true if the given line should be ignored.
// It checks if the same line or the previous line has an ignore comment.
// When an ignore is used, it marks the entry as used.
//
// Line matching logic:
//
//	Line N-1:  //zerologlintctx:ignore   ← matches line N
//	Line N:    log.Info().Msg("test")    ← target line
//
//	Line N:    log.Info().Msg("test") //zerologlintctx:ignore  ← also matches
func (m IgnoreMap) ShouldIgnore(line int) bool {
	if entry, onSameLine := m[line]; onSameLine {
		entry.used = true
		return true
	}
	if entry, onPrevLine := m[line-1]; onPrevLine {
		entry.used = true
		return true
	}
	return false
}

// GetUnusedIgnores returns the positions of ignore directives that were not used.
func (m IgnoreMap) GetUnusedIgnores() []token.Pos {
	var unused []token.Pos
	for _, entry := range m {
		if !entry.used {
			unused = append(unused, entry.pos)
		}
	}
	return unused
}

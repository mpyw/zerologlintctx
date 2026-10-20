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
// This directive can be placed at the end of the line of the code, or on a
// line of its own before it, to suppress warnings. A call chain that spans
// several lines is reached from the line it starts on, as well as from the
// line of the reported call.
//
// # Usage Examples
//
// Same-line ignore. It reaches its own line only:
//
//	log.Info().Msg("no ctx needed") //zerologlintctx:ignore
//
// Previous-line ignore:
//
//	//zerologlintctx:ignore
//	log.Info().
//	    Str("k", "v").
//	    Msg("no ctx needed")
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

// Ignores holds the ignore directives of one file, and what they can reach.
type Ignores struct {
	fset *token.FileSet
	// byLine holds each ignore directive, keyed by its unadjusted line.
	byLine map[int]*ignoreEntry
	// callStarts maps the "(" of each call to where its expression starts,
	// which is an earlier line for a chain that spans several.
	callStarts map[token.Pos]token.Pos
}

// Problem is a comment addressed to zerologlintctx that does nothing.
type Problem struct {
	Pos     token.Pos
	Message string
}

// Scan reads every comment of file once. It returns the ignore directives,
// and the comments that are addressed to zerologlintctx but do nothing:
// malformed directives, unknown directives, and ignore directives with text
// that is not a reason.
//
// Lines and columns are unadjusted throughout, so a //line directive cannot
// move an ignore away from its call.
func Scan(fset *token.FileSet, file *ast.File) (*Ignores, []Problem) {
	ignores := &Ignores{
		fset:       fset,
		byLine:     make(map[int]*ignoreEntry),
		callStarts: make(map[token.Pos]token.Pos),
	}
	codeStart := make(map[int]int) // Line → column of the first code on it
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case nil, *ast.CommentGroup, *ast.Comment:
			return false
		case *ast.File:
			return true
		case *ast.CallExpr:
			ignores.callStarts[n.Lparen] = n.Pos()
		}
		for _, pos := range []token.Pos{n.Pos(), n.End() - 1} {
			if !pos.IsValid() {
				continue
			}
			p := fset.PositionFor(pos, false)
			if col, ok := codeStart[p.Line]; !ok || p.Column < col {
				codeStart[p.Line] = p.Column
			}
		}
		return true
	})

	var problems []Problem
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			isIgnore, msg := classify(c.Text)
			switch {
			case isIgnore:
				p := fset.PositionFor(c.Pos(), false)
				col, hasCode := codeStart[p.Line]
				ignores.byLine[p.Line] = &ignoreEntry{
					pos:      c.Pos(),
					trailing: hasCode && col < p.Column,
				}
			case msg != "":
				problems = append(problems, Problem{Pos: c.Pos(), Message: msg})
			}
		}
	}
	return ignores, problems
}

// ignoreEntry tracks an ignore directive and whether it was used.
type ignoreEntry struct {
	pos      token.Pos // Position of the ignore comment
	trailing bool      // Whether it follows code on its line, and so reaches that line only
	used     bool      // Whether this ignore was actually used to suppress a warning
}

// classify reads text, one comment. isIgnore reports whether it is an ignore
// directive. Only the canonical form "//zerologlintctx:ignore" counts, with no
// space after "//". A reason may follow after "//" or " - "; other text may
// not.
//
// Otherwise problem says why text, a comment addressed to zerologlintctx,
// does nothing, or is "" when the comment is not addressed at all. A comment
// is addressed when its body, after "//" or "/*", starts with
// "zerologlintctx:" once leading whitespace is skipped. Prose that mentions
// zerologlintctx elsewhere in a comment is not addressed.
func classify(text string) (isIgnore bool, problem string) {
	d, ok := parse(text)
	switch {
	case !ok && isAddressed(text):
		return false, "malformed zerologlintctx directive: write it as //zerologlintctx:name"
	case !ok:
		return false, ""
	case d.Name != "ignore":
		return false, "unknown directive zerologlintctx:" + d.Name
	case !isReason(d.Args):
		return false, "zerologlintctx:ignore takes no argument; write a reason after //"
	}
	return true, ""
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
		if before, _, found := strings.Cut(body, "//"); found {
			text = "//" + before
		}
	}
	d, ok := ast.ParseDirective(token.NoPos, text)
	if !ok || d.Tool != directiveTool {
		return ast.Directive{}, false
	}
	return d, true
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

// ShouldIgnore reports whether a report at pos is ignored, and marks the
// ignore that reaches it as used.
//
// An ignore reaches the line it is on. One on a line of its own also reaches
// the line below. pos is reached from its own line and, when it is the "(" of
// a call, from the line where the call expression starts:
//
//	Line N-1:  //zerologlintctx:ignore   ← reaches line N
//	Line N:    log.Info().               ← the call expression starts here
//	Line N+1:      Msg("test")           ← pos, the "(" of Msg
//
//	Line N:    log.Info().Msg("test") //zerologlintctx:ignore  ← reaches line N only
func (ig *Ignores) ShouldIgnore(pos token.Pos) bool {
	lines := []int{ig.fset.PositionFor(pos, false).Line}
	if start, ok := ig.callStarts[pos]; ok {
		lines = append(lines, ig.fset.PositionFor(start, false).Line)
	}
	for _, line := range lines {
		if entry, onSameLine := ig.byLine[line]; onSameLine {
			entry.used = true
			return true
		}
		if entry, onPrevLine := ig.byLine[line-1]; onPrevLine && !entry.trailing {
			entry.used = true
			return true
		}
	}
	return false
}

// Unused returns the positions of ignore directives that were not used.
func (ig *Ignores) Unused() []token.Pos {
	var unused []token.Pos
	for _, entry := range ig.byLine {
		if !entry.used {
			unused = append(unused, entry.pos)
		}
	}
	return unused
}

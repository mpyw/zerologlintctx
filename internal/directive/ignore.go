// Package directive provides handling of zerologlintctx directive comments.
//
// # Supported Directives
//
// The package recognizes the following comment directive:
//
//	//zerologlintctx:ignore
//
// It follows the Go directive syntax: no space after "//" or after the colon.
// Free text, such as a reason, may follow after a space.
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
// "// zerologlintctx:ignore", is reported as malformed.
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
				line := fset.Position(c.Pos()).Line
				m[line] = &ignoreEntry{pos: c.Pos(), used: false}
			}
		}
	}
	return m
}

// isIgnoreComment checks if a comment is an ignore directive.
// Only the canonical Go directive form "//zerologlintctx:ignore" counts, with
// no space after "//". Free text may follow the name after a space.
func isIgnoreComment(text string) bool {
	d, ok := ast.ParseDirective(token.NoPos, text)
	return ok && d.Tool == directiveTool && d.Name == "ignore"
}

// FindMalformedDirectives returns the positions of comments that are
// addressed to zerologlintctx but are not a canonical directive.
//
// A comment is addressed when its body, after "//" or "/*", starts with
// "zerologlintctx:" once leading whitespace is skipped. Prose that mentions
// zerologlintctx elsewhere in a comment is not addressed.
func FindMalformedDirectives(file *ast.File) []token.Pos {
	var found []token.Pos
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if isMalformedDirective(c.Text) {
				found = append(found, c.Pos())
			}
		}
	}
	return found
}

// isMalformedDirective reports whether text is addressed to zerologlintctx but
// is not accepted by go/ast.ParseDirective as a zerologlintctx directive.
func isMalformedDirective(text string) bool {
	body, ok := strings.CutPrefix(text, "//")
	if !ok {
		body, ok = strings.CutPrefix(text, "/*")
	}
	if !ok || !strings.HasPrefix(strings.TrimLeftFunc(body, unicode.IsSpace), directiveTool+":") {
		return false
	}
	d, ok := ast.ParseDirective(token.NoPos, text)
	return !ok || d.Tool != directiveTool
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

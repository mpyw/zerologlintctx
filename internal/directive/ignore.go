// Package directive provides handling of zerologlintctx directive comments.
package directive

import (
	"go/ast"
	"go/token"
	"strings"
)

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
// Supports both "//zerologlintctx:ignore" and "// zerologlintctx:ignore".
func isIgnoreComment(text string) bool {
	text = strings.TrimPrefix(text, "//")
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "zerologlintctx:ignore")
}

// ShouldIgnore returns true if the given line should be ignored.
// It checks if the same line or the previous line has an ignore comment.
// When an ignore is used, it marks the entry as used.
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

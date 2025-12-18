package internal

import (
	"go/ast"
	"go/token"
	"strings"
)

// IgnoreMap tracks line numbers that have ignore comments.
type IgnoreMap map[int]struct{}

// BuildIgnoreMap scans a file for ignore comments and returns a map.
func BuildIgnoreMap(fset *token.FileSet, file *ast.File) IgnoreMap {
	m := make(IgnoreMap)
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if isIgnoreComment(c.Text) {
				line := fset.Position(c.Pos()).Line
				m[line] = struct{}{}
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
func (m IgnoreMap) ShouldIgnore(line int) bool {
	_, onSameLine := m[line]
	_, onPrevLine := m[line-1]
	return onSameLine || onPrevLine
}

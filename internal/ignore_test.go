package internal

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestBuildIgnoreMap(t *testing.T) {
	src := `package test

// zerologlintctx:ignore
func foo() {}

//zerologlintctx:ignore
func bar() {}

// some other comment
func baz() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	ignoreMap := BuildIgnoreMap(fset, file)

	// Should have 2 ignore directives (lines 3 and 6)
	if len(ignoreMap) != 2 {
		t.Errorf("expected 2 ignore entries, got %d", len(ignoreMap))
	}

	// Line 3 should be ignored (with space after //)
	if _, ok := ignoreMap[3]; !ok {
		t.Error("expected line 3 to have ignore directive")
	}

	// Line 6 should be ignored (without space after //)
	if _, ok := ignoreMap[6]; !ok {
		t.Error("expected line 6 to have ignore directive")
	}
}

func TestIgnoreMap_ShouldIgnore(t *testing.T) {
	src := `package test

// zerologlintctx:ignore
func foo() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	ignoreMap := BuildIgnoreMap(fset, file)

	tests := []struct {
		line     int
		expected bool
	}{
		{1, false}, // package line
		{2, false}, // empty line
		{3, true},  // same line as comment
		{4, true},  // next line after comment
		{5, false}, // two lines after comment
	}

	for _, tt := range tests {
		if got := ignoreMap.ShouldIgnore(tt.line); got != tt.expected {
			t.Errorf("ShouldIgnore(%d) = %v, expected %v", tt.line, got, tt.expected)
		}
	}
}

func TestIgnoreMap_GetUnusedIgnores(t *testing.T) {
	src := `package test

// zerologlintctx:ignore
func foo() {}

// zerologlintctx:ignore
func bar() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	ignoreMap := BuildIgnoreMap(fset, file)

	// Initially, all ignores are unused
	unused := ignoreMap.GetUnusedIgnores()
	if len(unused) != 2 {
		t.Errorf("expected 2 unused ignores, got %d", len(unused))
	}

	// Use one ignore
	ignoreMap.ShouldIgnore(4) // line 4 is after the first comment (line 3)

	// Now only one should be unused
	unused = ignoreMap.GetUnusedIgnores()
	if len(unused) != 1 {
		t.Errorf("expected 1 unused ignore after using one, got %d", len(unused))
	}
}

func TestIsIgnoreComment(t *testing.T) {
	tests := []struct {
		text     string
		expected bool
	}{
		{"//zerologlintctx:ignore", true},
		{"// zerologlintctx:ignore", true},
		{"//  zerologlintctx:ignore", true},
		{"// zerologlintctx:ignore some reason", true},
		{"// some other comment", false},
		{"//nolint", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := isIgnoreComment(tt.text); got != tt.expected {
				t.Errorf("isIgnoreComment(%q) = %v, expected %v", tt.text, got, tt.expected)
			}
		})
	}
}

func TestNilIgnoreMap(t *testing.T) {
	var m IgnoreMap

	// ShouldIgnore should handle nil map
	if m.ShouldIgnore(1) {
		t.Error("nil IgnoreMap.ShouldIgnore should return false")
	}

	// GetUnusedIgnores should handle nil map
	unused := m.GetUnusedIgnores()
	if unused != nil {
		t.Errorf("nil IgnoreMap.GetUnusedIgnores should return nil, got %v", unused)
	}
}

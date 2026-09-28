package directive

import "testing"

func TestIsIgnoreComment(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"//zerologlintctx:ignore", true},
		{"//zerologlintctx:ignore // reason", true},
		{"//zerologlintctx:ignore //reason", true},
		{"//zerologlintctx:ignore//reason", true},
		{"//zerologlintctx:ignore // want \"x\"", true},
		{"//zerologlintctx:ignore - intentionally not passing context", true},
		{"//zerologlintctx:ignore - reason // more", true},
		{"//zerologlintctx:ignore -", true},
		{"//zerologlintctx:ignore -reason", false},
		{"//zerologlintctx:ignore intentionally detached", false},
		{"//zerologlintctx:ignore-reason", false},
		{"//zerologlintctx:ignre", false},
		{"// zerologlintctx:ignore", false},
		{"//\tzerologlintctx:ignore", false},
		{"// zerologlintctx:ignore reason", false},
		{"//zerologlintctx: ignore", false},
		{"//zerologlintctx:ignored", false},
		{"//zerologlintctx:ignorefoo", false},
		{"//zerologlintctxx:ignore", false},
		{"//xzerologlintctx:ignore", false},
		{"//zerologlintctx:skip", false},
		{"//zerologlintctx", false},
		{"//nolint:foo //zerologlintctx:ignore", false},
		{"/* zerologlintctx:ignore */", false},
		{"// just a comment", false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := isIgnoreComment(tt.text); got != tt.want {
				t.Errorf("isIgnoreComment(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestProblem(t *testing.T) {
	const (
		malformed = "malformed zerologlintctx directive: write it as //zerologlintctx:name"
		argument  = "zerologlintctx:ignore takes no argument; write a reason after //"
	)
	tests := []struct {
		text string
		want string
	}{
		{"// zerologlintctx:ignore", malformed},
		{"//\tzerologlintctx:ignore", malformed},
		{"//zerologlintctx: ignore", malformed},
		{"/*zerologlintctx:ignore*/", malformed},
		{"/* zerologlintctx:ignore */", malformed},
		{"//zerologlintctx:Ignore", malformed},
		{"//zerologlintctx://reason", malformed},
		{"//zerologlintctx:ignre", "unknown directive zerologlintctx:ignre"},
		{"//zerologlintctx:ignore-reason", "unknown directive zerologlintctx:ignore-reason"},
		{"//zerologlintctx:ignored // reason", "unknown directive zerologlintctx:ignored"},
		{"//zerologlintctx:ignore intentionally detached", argument},
		{"//zerologlintctx:ignore intentionally // detached", argument},
		{"//zerologlintctx:ignore -reason", argument},
		{"//zerologlintctx:ignore - reason", ""},
		{"//zerologlintctx:ignore - reason // more", ""},
		{"//zerologlintctx:ignore", ""},
		{"//zerologlintctx:ignore // reason", ""},
		{"//zerologlintctx:ignore //reason", ""},
		{"//zerologlintctx:ignore//reason", ""},
		{"//zerologlintctx:ignore // want \"x\"", ""},
		{"//nolint:foo //zerologlintctx:ignore", ""},
		{"//go:generate zerologlintctx:ignore", ""},
		{"// Use zerologlintctx:ignore to suppress a report.", ""},
		{"// just a comment", ""},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := problem(tt.text); got != tt.want {
				t.Errorf("problem(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

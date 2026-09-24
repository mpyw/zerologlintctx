package directive

import "testing"

func TestIsIgnoreComment(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"//zerologlintctx:ignore", true},
		{"//zerologlintctx:ignore - intentionally not passing context", true},
		{"//zerologlintctx:ignore // reason", true},
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

func TestIsMalformedDirective(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"// zerologlintctx:ignore", true},
		{"//zerologlintctx: ignore", true},
		{"/*zerologlintctx:ignore*/", true},
		{"//zerologlintctx:Ignore", true},
		{"//zerologlintctx:ignore", false},
		{"//zerologlintctx:ignore - reason", false},
		{"// Use zerologlintctx:ignore to suppress a report.", false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := isMalformedDirective(tt.text); got != tt.want {
				t.Errorf("isMalformedDirective(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

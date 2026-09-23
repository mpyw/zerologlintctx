package directive

import "testing"

func TestIsIgnoreComment(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"//zerologlintctx:ignore", true},
		{"// zerologlintctx:ignore", true},
		{"//\tzerologlintctx:ignore", true},
		{"//zerologlintctx:ignore - intentionally not passing context", true},
		{"//zerologlintctx:ignore // reason", true},
		{"// zerologlintctx:ignore reason", true},
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

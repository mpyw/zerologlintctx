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

func TestMalformedDirective(t *testing.T) {
	const suggest = "//zerologlintctx:ignore"
	tests := []struct {
		text           string
		wantSuggestion string
		wantOK         bool
	}{
		{"// zerologlintctx:ignore", suggest, true},
		{"//\tzerologlintctx:ignore", suggest, true},
		{"// zerologlintctx:ignore - reason", suggest, true},
		{"//zerologlintctx: ignore", suggest, true},
		{"/*zerologlintctx:ignore*/", suggest, true},
		{"/* zerologlintctx:ignore */", suggest, true},
		{"// zerologlintctx:skip", "//zerologlintctx:skip", true},
		{"//zerologlintctx:Ignore", "", true},
		{"// zerologlintctx:Ignore", "", true},
		{"//zerologlintctx:", "", true},
		{"// zerologlintctx:", "", true},
		{"/* zerologlintctx: */", "", true},
		{"//zerologlintctx:ignore", "", false},
		{"//zerologlintctx:ignore - reason", "", false},
		{"//zerologlintctx:ignored", "", false},
		{"// Use zerologlintctx:ignore to suppress a report.", "", false},
		{"// zerologlintctxx:ignore", "", false},
		{"// just a comment", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			suggestion, ok := malformedDirective(tt.text)
			if suggestion != tt.wantSuggestion || ok != tt.wantOK {
				t.Errorf("malformedDirective(%q) = (%q, %v), want (%q, %v)",
					tt.text, suggestion, ok, tt.wantSuggestion, tt.wantOK)
			}
		})
	}
}

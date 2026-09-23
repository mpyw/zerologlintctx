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

func TestMalformedDirectiveName(t *testing.T) {
	tests := []struct {
		text     string
		wantName string
		wantOK   bool
	}{
		{"// zerologlintctx:ignore", "ignore", true},
		{"//\tzerologlintctx:ignore", "ignore", true},
		{"// zerologlintctx:ignore - reason", "ignore", true},
		{"//zerologlintctx: ignore", "ignore", true},
		{"//zerologlintctx:Ignore", "ignore", true},
		{"/*zerologlintctx:ignore*/", "ignore", true},
		{"/* zerologlintctx:ignore */", "ignore", true},
		{"// zerologlintctx:", "ignore", true},
		{"//zerologlintctx:ignore", "", false},
		{"//zerologlintctx:ignore - reason", "", false},
		{"//zerologlintctx:ignored", "", false},
		{"// Use zerologlintctx:ignore to suppress a report.", "", false},
		{"// zerologlintctxx:ignore", "", false},
		{"// just a comment", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			name, ok := malformedDirectiveName(tt.text)
			if name != tt.wantName || ok != tt.wantOK {
				t.Errorf("malformedDirectiveName(%q) = (%q, %v), want (%q, %v)",
					tt.text, name, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}

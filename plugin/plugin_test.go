package plugin_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis/analysistest"

	_ "github.com/mpyw/zerologlintctx/plugin"
)

func TestPluginReports(t *testing.T) {
	for _, settings := range []any{nil, map[string]any{}} {
		p, err := newPlugin(t)(settings)
		if err != nil {
			t.Fatalf("settings %v: %v", settings, err)
		}
		as, err := p.BuildAnalyzers()
		if err != nil {
			t.Fatal(err)
		}
		if len(as) != 1 {
			t.Fatalf("got %d analyzers, want 1", len(as))
		}
		dir, err := filepath.Abs(filepath.Join("..", "testdata"))
		if err != nil {
			t.Fatal(err)
		}
		analysistest.Run(t, dir, as[0], "zerolog")
	}
}

func TestPluginRejectsSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings any
	}{
		{"any key", map[string]any{"test": false}},
		{"settings that are not a map", []any{"x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newPlugin(t)(tt.settings)
			if err == nil || !strings.Contains(err.Error(), "reading settings") {
				t.Fatalf("got error %v, want a settings error", err)
			}
		})
	}
}

func TestPluginLoadMode(t *testing.T) {
	p, err := newPlugin(t)(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.GetLoadMode(); got != register.LoadModeTypesInfo {
		t.Errorf("load mode %q, want %q", got, register.LoadModeTypesInfo)
	}
}

// newPlugin finds the constructor the package registered.
func newPlugin(t *testing.T) register.NewPlugin {
	t.Helper()
	np, err := register.GetPlugin("zerologlintctx")
	if err != nil {
		t.Fatal(err)
	}
	return np
}

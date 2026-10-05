// Package plugin registers zerologlintctx as a golangci-lint module plugin.
//
// Import it from .custom-gcl.yml to build a golangci-lint binary that holds
// zerologlintctx. The analyzer has no settings, so the settings block in
// .golangci.yml must be empty or left out.
package plugin

import (
	"fmt"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"

	"github.com/mpyw/zerologlintctx"
)

func init() {
	register.Plugin(zerologlintctx.Analyzer.Name, newPlugin)
}

// pluginSettings is the settings block. It has no fields, so any key is
// rejected rather than ignored.
type pluginSettings struct{}

// pluginAnalyzers is the plugin built from one settings block.
type pluginAnalyzers struct{}

func newPlugin(settings any) (register.LinterPlugin, error) {
	if _, err := register.DecodeSettings[pluginSettings](settings); err != nil {
		return nil, fmt.Errorf("reading settings: %w", err)
	}
	return pluginAnalyzers{}, nil
}

// BuildAnalyzers gives the zerologlintctx analyzer. It has no flags, so it is
// shared as it is.
func (pluginAnalyzers) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{zerologlintctx.Analyzer}, nil
}

// GetLoadMode asks for type information, which buildssa needs.
func (pluginAnalyzers) GetLoadMode() string {
	return register.LoadModeTypesInfo
}

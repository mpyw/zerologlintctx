package internal

import (
	"go/types"
	"testing"
)

func TestParseContextCarriers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []ContextCarrier
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:  "single carrier",
			input: "github.com/labstack/echo/v4.Context",
			expected: []ContextCarrier{
				{PkgPath: "github.com/labstack/echo/v4", TypeName: "Context"},
			},
		},
		{
			name:  "multiple carriers",
			input: "github.com/labstack/echo/v4.Context,github.com/gin-gonic/gin.Context",
			expected: []ContextCarrier{
				{PkgPath: "github.com/labstack/echo/v4", TypeName: "Context"},
				{PkgPath: "github.com/gin-gonic/gin", TypeName: "Context"},
			},
		},
		{
			name:  "with spaces",
			input: "  github.com/labstack/echo/v4.Context , github.com/gin-gonic/gin.Context  ",
			expected: []ContextCarrier{
				{PkgPath: "github.com/labstack/echo/v4", TypeName: "Context"},
				{PkgPath: "github.com/gin-gonic/gin", TypeName: "Context"},
			},
		},
		{
			name:     "invalid format without dot",
			input:    "invalidformat",
			expected: []ContextCarrier{},
		},
		{
			name:  "mixed valid and invalid",
			input: "github.com/valid.Type,,invalid,github.com/another.Type",
			expected: []ContextCarrier{
				{PkgPath: "github.com/valid", TypeName: "Type"},
				{PkgPath: "github.com/another", TypeName: "Type"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseContextCarriers(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("ParseContextCarriers(%q) returned %d carriers, expected %d",
					tt.input, len(result), len(tt.expected))
				return
			}
			for i, carrier := range result {
				if carrier.PkgPath != tt.expected[i].PkgPath {
					t.Errorf("carrier[%d].PkgPath = %q, expected %q",
						i, carrier.PkgPath, tt.expected[i].PkgPath)
				}
				if carrier.TypeName != tt.expected[i].TypeName {
					t.Errorf("carrier[%d].TypeName = %q, expected %q",
						i, carrier.TypeName, tt.expected[i].TypeName)
				}
			}
		})
	}
}

func TestIsTerminatorMethod(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"Msg", true},
		{"Msgf", true},
		{"MsgFunc", true},
		{"Send", true},
		{"Info", false},
		{"Debug", false},
		{"Str", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTerminatorMethod(tt.name); got != tt.expected {
				t.Errorf("isTerminatorMethod(%q) = %v, expected %v", tt.name, got, tt.expected)
			}
		})
	}
}

func TestIsLogLevelMethod(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"Info", true},
		{"Debug", true},
		{"Warn", true},
		{"Error", true},
		{"Fatal", true},
		{"Panic", true},
		{"Trace", true},
		{"Log", true},
		{"Msg", false},
		{"Str", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLogLevelMethod(tt.name); got != tt.expected {
				t.Errorf("isLogLevelMethod(%q) = %v, expected %v", tt.name, got, tt.expected)
			}
		})
	}
}

func TestIsContextOrCarrierType(t *testing.T) {
	// Create a mock context.Context type
	contextPkg := types.NewPackage("context", "context")
	contextTypeName := types.NewTypeName(0, contextPkg, "Context", nil)
	contextInterface := types.NewInterfaceType(nil, nil)
	contextInterface.Complete()
	contextNamed := types.NewNamed(contextTypeName, contextInterface, nil)

	// Create a mock carrier type (e.g., echo.Context)
	echoPkg := types.NewPackage("github.com/labstack/echo/v4", "echo")
	echoTypeName := types.NewTypeName(0, echoPkg, "Context", nil)
	echoInterface := types.NewInterfaceType(nil, nil)
	echoInterface.Complete()
	echoNamed := types.NewNamed(echoTypeName, echoInterface, nil)

	// Create a non-matching type
	otherPkg := types.NewPackage("other/pkg", "pkg")
	otherTypeName := types.NewTypeName(0, otherPkg, "Other", nil)
	otherStruct := types.NewStruct(nil, nil)
	otherNamed := types.NewNamed(otherTypeName, otherStruct, nil)

	carriers := []ContextCarrier{
		{PkgPath: "github.com/labstack/echo/v4", TypeName: "Context"},
	}

	tests := []struct {
		name     string
		typ      types.Type
		carriers []ContextCarrier
		expected bool
	}{
		{
			name:     "context.Context",
			typ:      contextNamed,
			carriers: carriers,
			expected: true,
		},
		{
			name:     "echo.Context carrier",
			typ:      echoNamed,
			carriers: carriers,
			expected: true,
		},
		{
			name:     "non-matching type",
			typ:      otherNamed,
			carriers: carriers,
			expected: false,
		},
		{
			name:     "nil carriers",
			typ:      otherNamed,
			carriers: nil,
			expected: false,
		},
		{
			name:     "pointer to context.Context",
			typ:      types.NewPointer(contextNamed),
			carriers: carriers,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsContextOrCarrierType(tt.typ, tt.carriers); got != tt.expected {
				t.Errorf("IsContextOrCarrierType() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestUnwrapPointer(t *testing.T) {
	// Create a basic type
	basicType := types.Typ[types.Int]

	// Create a pointer to the basic type
	ptrType := types.NewPointer(basicType)

	tests := []struct {
		name     string
		typ      types.Type
		expected types.Type
	}{
		{
			name:     "non-pointer type",
			typ:      basicType,
			expected: basicType,
		},
		{
			name:     "pointer type",
			typ:      ptrType,
			expected: basicType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unwrapPointer(tt.typ); got != tt.expected {
				t.Errorf("unwrapPointer() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

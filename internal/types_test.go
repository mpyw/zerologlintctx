package internal

import (
	"go/types"
	"testing"
)

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

func TestIsContextType(t *testing.T) {
	// Create a mock context.Context type
	contextPkg := types.NewPackage("context", "context")
	contextTypeName := types.NewTypeName(0, contextPkg, "Context", nil)
	contextInterface := types.NewInterfaceType(nil, nil)
	contextInterface.Complete()
	contextNamed := types.NewNamed(contextTypeName, contextInterface, nil)

	// Create a non-matching type
	otherPkg := types.NewPackage("other/pkg", "pkg")
	otherTypeName := types.NewTypeName(0, otherPkg, "Other", nil)
	otherStruct := types.NewStruct(nil, nil)
	otherNamed := types.NewNamed(otherTypeName, otherStruct, nil)

	tests := []struct {
		name     string
		typ      types.Type
		expected bool
	}{
		{
			name:     "context.Context",
			typ:      contextNamed,
			expected: true,
		},
		{
			name:     "non-context type",
			typ:      otherNamed,
			expected: false,
		},
		{
			name:     "pointer to context.Context",
			typ:      types.NewPointer(contextNamed),
			expected: true,
		},
		{
			name:     "basic type",
			typ:      types.Typ[types.Int],
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsContextType(tt.typ); got != tt.expected {
				t.Errorf("IsContextType() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

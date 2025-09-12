package codegen

import (
	"strings"
	"testing"
)

func TestBuilder_Basic(t *testing.T) {
	builder := NewBuilder("example")

	// Add a simple type using CreateType
	myStringType := builder.CreateType("MyString", "string")
	if myStringType.Name != "MyString" {
		t.Errorf("Expected type name MyString, got %s", myStringType.Name)
	}

	// Add a struct
	person := builder.CreateStruct("Person")
	person.WithField("Name", "string", `json:"name"`)
	person.WithField("Age", "int", `json:"age"`)

	// Generate and check
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Builder.Build() failed: %v", err)
	}

	generatedStr := string(generated)

	// Basic checks
	if !strings.Contains(generatedStr, "package example") {
		t.Error("Expected package declaration")
	}
	if !strings.Contains(generatedStr, "type MyString string") {
		t.Error("Expected type declaration")
	}
	if !strings.Contains(generatedStr, "type Person struct") {
		t.Error("Expected struct declaration")
	}
}

func TestBuilder_Enum(t *testing.T) {
	builder := NewBuilder("test")

	// Create enum type using CreateType and CreateConstBlock
	statusType := builder.CreateType("Status", "string")
	if statusType.Name != "Status" {
		t.Errorf("Expected type name Status, got %s", statusType.Name)
	}

	// Create constants using CreateConstBlock
	constants := builder.CreateConstBlock()
	constants.AddConst("StatusActive", "Status", `"active"`, "Active status")
	constants.AddConst("StatusInactive", "Status", `"inactive"`, "Inactive status")

	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Builder.Build() failed: %v", err)
	}

	generatedStr := string(generated)

	// Check enum generation
	if !strings.Contains(generatedStr, "type Status string") {
		t.Error("Expected enum type declaration")
	}
	if !strings.Contains(generatedStr, "StatusActive Status") {
		t.Error("Expected enum constant")
	}
	if !strings.Contains(generatedStr, "StatusInactive Status") {
		t.Error("Expected enum constant")
	}
}

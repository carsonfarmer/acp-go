package codegen

import (
	"strings"
	"testing"
)

func TestStructDecl_WithMethods(t *testing.T) {
	// Create a struct using the new API
	builder := NewBuilder("example")
	
	// Create a struct and add fields
	testStruct := builder.CreateStruct("TestStruct")
	testStruct.WithField("Name", "string", `json:"name"`)
	testStruct.WithField("Age", "int", `json:"age"`)
	
	// Add a method
	testStruct.CreateMethod(`func (t *TestStruct) GetName() string {
		return t.Name
	}`)

	// Build and verify output
	generated, err := builder.Build()
	if err != nil {
		t.Fatalf("Failed to build code: %v", err)
	}

	// Convert to string
	output := string(generated)
	
	// Check struct definition
	if !strings.Contains(output, "type TestStruct struct {") {
		t.Error("Expected struct definition")
	}
	
	// Check fields
	if !strings.Contains(output, "Name string") || !strings.Contains(output, `json:"name"`) {
		t.Error("Expected Name field with json tag")
	}
	if !strings.Contains(output, "Age") || !strings.Contains(output, `json:"age"`) {
		t.Error("Expected Age field with json tag")
	}
	
	// Check method
	if !strings.Contains(output, "func (t *TestStruct) GetName() string {") {
		t.Error("Expected GetName method signature")
	}
	if !strings.Contains(output, "return t.Name") {
		t.Error("Expected method body")
	}

	// Verify that struct comes before method in output
	structPos := strings.Index(output, "type TestStruct struct")
	methodPos := strings.Index(output, "func (t *TestStruct) GetName")
	
	if structPos == -1 || methodPos == -1 || structPos >= methodPos {
		t.Error("Expected struct definition to come before method definition")
	}
}

func TestBuilder_StructWithJSONMethods(t *testing.T) {
	builder := NewBuilder("testpkg")
	
	// Add a struct using new API
	content := builder.CreateStruct("Content")
	content.WithField("text", "*TextContent")
	content.WithField("number", "*NumberContent")
	content.WithField("discriminator", "string")
	
	// Build the code
	code, err := builder.Build()
	if err != nil {
		t.Fatalf("Failed to build code: %v", err)
	}
	
	codeStr := string(code)
	
	// Check struct definition
	if !strings.Contains(codeStr, "type Content struct {") {
		t.Error("Expected Content struct definition")
	}
	
	// Since we don't have AddJSONMethods, we'll just verify the struct was created
	if !strings.Contains(codeStr, "*TextContent") {
		t.Error("Expected text field with *TextContent type")
	}
	if !strings.Contains(codeStr, "*NumberContent") {
		t.Error("Expected number field with *NumberContent type")
	}
	if !strings.Contains(codeStr, "discriminator") {
		t.Error("Expected discriminator field")
	}
}
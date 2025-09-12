package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerator_LoadSchema(t *testing.T) {
	testDir := "testdata_gen"
	validSchemaFile := filepath.Join(testDir, "valid.json")
	invalidSchemaFile := filepath.Join(testDir, "invalid.json")
	
	defer os.RemoveAll(testDir)
	
	// Setup test files
	os.MkdirAll(testDir, 0755)
	os.WriteFile(validSchemaFile, []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		}
	}`), 0644)
	os.WriteFile(invalidSchemaFile, []byte(`{invalid json`), 0644)

	tests := []struct {
		name       string
		configFile string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "valid schema",
			configFile: validSchemaFile,
			wantErr:    false,
		},
		{
			name:       "invalid schema",
			configFile: invalidSchemaFile,
			wantErr:    true,
			errMsg:     "failed to compile schema",
		},
		{
			name:       "non-existent file",
			configFile: "non/existent/file.json",
			wantErr:    true,
			errMsg:     "failed to read schema file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				InputFile:   tt.configFile,
				OutputFile:  "output.go",
				PackageName: "test",
			}
			
			generator := NewGenerator(config)
			err := generator.LoadSchema()
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Generator.LoadSchema() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Generator.LoadSchema() error = %v, want error containing %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Generator.LoadSchema() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				
				if generator.schema == nil {
					t.Error("Generator.LoadSchema() schema is nil after successful load")
				}
			}
		})
	}
}

func TestGenerator_Generate(t *testing.T) {
	testDir := "testdata_gen2"
	schemaFile := filepath.Join(testDir, "enum.json")
	
	defer os.RemoveAll(testDir)
	
	// Setup test schema with enum
	os.MkdirAll(testDir, 0755)
	os.WriteFile(schemaFile, []byte(`{
		"$defs": {
			"Status": {
				"type": "string",
				"oneOf": [
					{
						"const": "active",
						"description": "User is active"
					},
					{
						"const": "inactive", 
						"description": "User is inactive"
					}
				]
			}
		}
	}`), 0644)

	config := &Config{
		InputFile:   schemaFile,
		OutputFile:  "output.go",
		PackageName: "test",
	}
	
	generator := NewGenerator(config)
	
	// Load schema first
	err := generator.LoadSchema()
	if err != nil {
		t.Fatalf("Failed to load schema: %v", err)
	}
	
	// Test generation
	err = generator.Generate()
	if err != nil {
		t.Errorf("Generator.Generate() error = %v", err)
	}
	
	// Test generation without loaded schema
	generator2 := NewGenerator(config)
	err = generator2.Generate()
	if err == nil {
		t.Error("Generator.Generate() should fail when schema not loaded")
	}
	if !strings.Contains(err.Error(), "schema not loaded") {
		t.Errorf("Generator.Generate() error = %v, want error about schema not loaded", err)
	}
}

func TestGenerator_SaveToFile(t *testing.T) {
	testDir := "testdata_gen3"
	outputDir := "output_gen"
	schemaFile := filepath.Join(testDir, "simple.json")
	outputFile := filepath.Join(outputDir, "types.go")
	
	defer func() {
		os.RemoveAll(testDir)
		os.RemoveAll(outputDir)
	}()
	
	// Setup
	os.MkdirAll(testDir, 0755)
	os.WriteFile(schemaFile, []byte(`{
		"$defs": {
			"SimpleType": {
				"type": "string"
			}
		}
	}`), 0644)

	config := &Config{
		InputFile:   schemaFile,
		OutputFile:  outputFile,
		PackageName: "test",
	}
	
	// Validate config to create output directory
	err := config.Validate()
	if err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}
	
	generator := NewGenerator(config)
	
	// Load and generate
	err = generator.LoadSchema()
	if err != nil {
		t.Fatalf("Failed to load schema: %v", err)
	}
	
	err = generator.Generate()
	if err != nil {
		t.Fatalf("Failed to generate: %v", err)
	}
	
	// Test save
	err = generator.SaveToFile()
	if err != nil {
		t.Errorf("Generator.SaveToFile() error = %v", err)
		return
	}
	
	// Verify file was created
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Error("Output file was not created")
	}
	
	// Verify file content
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Errorf("Failed to read output file: %v", err)
		return
	}
	
	contentStr := string(content)
	if !strings.Contains(contentStr, "package test") {
		t.Error("Output file should contain correct package declaration")
	}
	
	if !strings.Contains(contentStr, "SimpleType") {
		t.Error("Output file should contain generated type")
	}
}

func TestGenerator_StructFieldsAlphabeticalOrder(t *testing.T) {
	testDir := "testdata_order"
	schemaFile := filepath.Join(testDir, "test.json")
	outputFile := filepath.Join(testDir, "output.go")
	
	defer os.RemoveAll(testDir)
	
	// Schema with properties in non-alphabetical order
	os.MkdirAll(testDir, 0755)
	os.WriteFile(schemaFile, []byte(`{
		"$defs": {
			"TestStruct": {
				"type": "object",
				"properties": {
					"zField": {"type": "string", "description": "Z field"},
					"aField": {"type": "string", "description": "A field"}, 
					"mField": {"type": "string", "description": "M field"},
					"bField": {"type": "string", "description": "B field"}
				}
			}
		}
	}`), 0644)

	config := &Config{
		InputFile:   schemaFile,
		OutputFile:  outputFile,
		PackageName: "test",
	}
	
	err := config.Validate()
	if err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}
	
	generator := NewGenerator(config)
	
	err = generator.LoadSchema()
	if err != nil {
		t.Fatalf("Failed to load schema: %v", err)
	}
	
	err = generator.Generate()
	if err != nil {
		t.Fatalf("Failed to generate: %v", err)
	}
	
	err = generator.SaveToFile()
	if err != nil {
		t.Errorf("Generator.SaveToFile() error = %v", err)
		return
	}
	
	// Read and verify field order
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Errorf("Failed to read output file: %v", err)
		return
	}
	
	contentStr := string(content)
	
	// Find positions of each field in the generated code  
	aFieldPos := strings.Index(contentStr, "AField")
	bFieldPos := strings.Index(contentStr, "BField")  
	mFieldPos := strings.Index(contentStr, "MField")
	zFieldPos := strings.Index(contentStr, "ZField")
	
	if aFieldPos == -1 || bFieldPos == -1 || mFieldPos == -1 || zFieldPos == -1 {
		t.Logf("Generated content:\n%s", contentStr)
		t.Error("Not all fields found in generated code")
		return
	}
	
	// Verify alphabetical order: Afield < Bfield < Mfield < Zfield
	if !(aFieldPos < bFieldPos && bFieldPos < mFieldPos && mFieldPos < zFieldPos) {
		t.Errorf("Fields are not in alphabetical order. Positions: A=%d, B=%d, M=%d, Z=%d", 
			aFieldPos, bFieldPos, mFieldPos, zFieldPos)
	}
}

func TestGenerator_OneOfWithDiscriminator(t *testing.T) {
	testDir := "testdata_oneof"
	schemaFile := filepath.Join(testDir, "test.json")
	outputFile := filepath.Join(testDir, "output.go")
	
	defer os.RemoveAll(testDir)
	
	// Schema with OneOf using type discriminator
	os.MkdirAll(testDir, 0755)
	os.WriteFile(schemaFile, []byte(`{
		"$defs": {
			"TestOneOf": {
				"description": "Test OneOf with type discriminator",
				"oneOf": [
					{
						"description": "Text variant",
						"properties": {
							"text": {"type": "string"},
							"type": {"const": "text", "type": "string"}
						},
						"required": ["type", "text"],
						"type": "object"
					},
					{
						"description": "Number variant", 
						"properties": {
							"number": {"type": "integer"},
							"type": {"const": "number", "type": "string"}
						},
						"required": ["type", "number"],
						"type": "object"
					}
				]
			}
		}
	}`), 0644)

	config := &Config{
		InputFile:   schemaFile,
		OutputFile:  outputFile,
		PackageName: "test",
	}
	
	err := config.Validate()
	if err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}
	
	generator := NewGenerator(config)
	
	err = generator.LoadSchema()
	if err != nil {
		t.Fatalf("Failed to load schema: %v", err)
	}
	
	err = generator.Generate()
	if err != nil {
		t.Fatalf("Failed to generate: %v", err)
	}
	
	err = generator.SaveToFile()
	if err != nil {
		t.Errorf("Generator.SaveToFile() error = %v", err)
		return
	}
	
	// Read and verify OneOf structure
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Errorf("Failed to read output file: %v", err)
		return
	}
	
	contentStr := string(content)
	
	// Verify discriminator field exists and is private (no JSON tag)
	if !strings.Contains(contentStr, "discriminator string") {
		t.Error("Expected private discriminator field not found")
	}
	// Ensure discriminator field doesn't have JSON tag
	if strings.Contains(contentStr, "discriminator string `json:") {
		t.Error("Discriminator field should not have JSON tag")
	}
	
	// Verify pointer fields
	if !strings.Contains(contentStr, "*TestOneOfText") {
		t.Error("Expected pointer field *TestOneOfText not found")
	}
	if !strings.Contains(contentStr, "*TestOneOfNumber") {
		t.Error("Expected pointer field *TestOneOfNumber not found")  
	}
	
	// Verify variant structs were generated
	if !strings.Contains(contentStr, "type TestOneOfText struct") {
		t.Error("Expected TestOneOfText struct not found")
	}
	if !strings.Contains(contentStr, "type TestOneOfNumber struct") {
		t.Error("Expected TestOneOfNumber struct not found")
	}
}

func TestGenerator_OneOfWithJSONMethods(t *testing.T) {
	testDir := "testdata_json"
	schemaFile := filepath.Join(testDir, "test.json")
	outputFile := filepath.Join(testDir, "output.go")
	
	defer os.RemoveAll(testDir)
	
	// Schema with OneOf for JSON methods testing
	os.MkdirAll(testDir, 0755)
	os.WriteFile(schemaFile, []byte(`{
		"$defs": {
			"TestOneOf": {
				"description": "Test OneOf with JSON methods",
				"oneOf": [
					{
						"description": "Text variant",
						"properties": {
							"text": {"type": "string"},
							"type": {"const": "text", "type": "string"}
						},
						"required": ["type", "text"],
						"type": "object"
					},
					{
						"description": "Number variant", 
						"properties": {
							"number": {"type": "integer"},
							"type": {"const": "number", "type": "string"}
						},
						"required": ["type", "number"],
						"type": "object"
					}
				]
			}
		}
	}`), 0644)

	config := &Config{
		InputFile:   schemaFile,
		OutputFile:  outputFile,
		PackageName: "test",
	}
	
	err := config.Validate()
	if err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}
	
	generator := NewGenerator(config)
	
	err = generator.LoadSchema()
	if err != nil {
		t.Fatalf("Failed to load schema: %v", err)
	}
	
	err = generator.Generate()
	if err != nil {
		t.Fatalf("Failed to generate: %v", err)
	}
	
	err = generator.SaveToFile()
	if err != nil {
		t.Errorf("Generator.SaveToFile() error = %v", err)
		return
	}
	
	// Read and verify JSON methods were generated
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Errorf("Failed to read output file: %v", err)
		return
	}
	
	contentStr := string(content)
	
	// Verify private fields
	if !strings.Contains(contentStr, "text          *TestOneOfText") {
		t.Error("Expected private text field not found")
	}
	if !strings.Contains(contentStr, "number        *TestOneOfNumber") {
		t.Error("Expected private number field not found")
	}
	
	// Verify JSON methods (the receiver variable name is generated based on the type name)
	if !strings.Contains(contentStr, "func (t TestOneOf) MarshalJSON() ([]byte, error)") {
		t.Error("Expected MarshalJSON method not found")
	}
	if !strings.Contains(contentStr, "func (t *TestOneOf) UnmarshalJSON(data []byte) error") {
		t.Error("Expected UnmarshalJSON method not found")
	}
	
	// Verify imports
	if !strings.Contains(contentStr, `"encoding/json"`) {
		t.Error("Expected encoding/json import not found")
	}
	if !strings.Contains(contentStr, `"fmt"`) {
		t.Error("Expected fmt import not found")
	}
}

func TestNewGenerator(t *testing.T) {
	config := &Config{
		InputFile:   "input.json",
		OutputFile:  "output.go",
		PackageName: "test",
	}
	
	generator := NewGenerator(config)
	
	if generator == nil {
		t.Fatal("NewGenerator() returned nil")
	}
	
	if generator.config != config {
		t.Error("NewGenerator() config not set correctly")
	}
	
	if generator.builder == nil {
		t.Error("NewGenerator() builder not initialized")
	}
	
	if generator.schema != nil {
		t.Error("NewGenerator() schema should be nil initially")
	}
}
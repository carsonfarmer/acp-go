package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerator_FieldTypes(t *testing.T) {
	testDir := "testdata_field_types"
	schemaFile := filepath.Join(testDir, "test.json")
	outputFile := filepath.Join(testDir, "output.go")
	
	defer os.RemoveAll(testDir)
	
	// Comprehensive schema testing various field type scenarios
	os.MkdirAll(testDir, 0755)
	os.WriteFile(schemaFile, []byte(`{
		"$defs": {
			"RefType": {
				"type": "object",
				"properties": {
					"enabled": {"type": "boolean", "default": false}
				}
			},
			"User": {
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "User ID"},
					"name": {"type": "string", "description": "User name"},
					"email": {"type": ["string", "null"], "description": "User email (nullable)"},
					"profile": {"type": "string", "description": "User profile (optional)"},
					"tags": {"type": "array", "items": {"type": "string"}, "description": "User tags"},
					"nullableInt": {"type": ["integer", "null"], "description": "Nullable integer field"},
					"nullableBool": {"type": ["boolean", "null"], "description": "Nullable boolean field"},
					"nullableNumber": {"type": ["number", "null"], "description": "Nullable number field"},
					"optionalRef": {
						"$ref": "#/$defs/RefType",
						"description": "Optional reference field"
					},
					"requiredRef": {
						"$ref": "#/$defs/RefType", 
						"description": "Required reference field"
					}
				},
				"required": ["id", "name", "requiredRef"]
			},
			"OptionalStruct": {
				"type": "object",
				"properties": {
					"field1": {"type": "string"},
					"field2": {"type": "integer"},
					"field3": {"type": ["string", "null"]}
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
	
	// Read and verify field types
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Errorf("Failed to read output file: %v", err)
		return
	}
	
	contentStr := string(content)
	
	t.Run("NullableStringHandling", func(t *testing.T) {
		// Nullable string should NOT use pointer type
		if strings.Contains(contentStr, "*string") {
			t.Error("Nullable string field should not use pointer type (*string)")
		}
		if !strings.Contains(contentStr, "Email          string") {
			t.Error("Expected Email field to be of type 'string', not '*string'")
		}
		
		// Other nullable types should still use pointer types
		if !strings.Contains(contentStr, "*int64") {
			t.Error("Nullable integer field should use pointer type (*int64)")
		}
		if !strings.Contains(contentStr, "*bool") {
			t.Error("Nullable boolean field should use pointer type (*bool)")
		}
		if !strings.Contains(contentStr, "*float64") {
			t.Error("Nullable number field should use pointer type (*float64)")
		}
	})
	
	t.Run("OmitemptyTags", func(t *testing.T) {
		// Required fields should NOT have omitempty
		if !strings.Contains(contentStr, `json:"id"`) || strings.Contains(contentStr, `json:"id,omitempty"`) {
			t.Error("Required field 'id' should not have omitempty")
		}
		if !strings.Contains(contentStr, `json:"name"`) || strings.Contains(contentStr, `json:"name,omitempty"`) {
			t.Error("Required field 'name' should not have omitempty")
		}
		
		// Optional/nullable fields should have omitempty
		if !strings.Contains(contentStr, `json:"email,omitempty"`) {
			t.Error("Nullable field 'email' should have omitempty")
		}
		if !strings.Contains(contentStr, `json:"profile,omitempty"`) {
			t.Error("Optional field 'profile' should have omitempty")
		}
		if !strings.Contains(contentStr, `json:"tags,omitempty"`) {
			t.Error("Optional field 'tags' should have omitempty")
		}
		if !strings.Contains(contentStr, `json:"nullableInt,omitempty"`) {
			t.Error("Nullable int should have omitempty tag")
		}
		if !strings.Contains(contentStr, `json:"nullableBool,omitempty"`) {
			t.Error("Nullable bool should have omitempty tag")
		}
		if !strings.Contains(contentStr, `json:"nullableNumber,omitempty"`) {
			t.Error("Nullable number should have omitempty tag")
		}
	})
	
	t.Run("OptionalRefFields", func(t *testing.T) {
		// Optional reference field should be a pointer type
		if !strings.Contains(contentStr, "*RefType") {
			t.Error("Optional reference field should be a pointer type (*RefType)")
			t.Logf("Generated content:\n%s", contentStr)
		}
		
		// Required reference field should not be a pointer type
		if !strings.Contains(contentStr, "RequiredRef    RefType") {
			t.Error("Required reference field should not be a pointer type")
			t.Logf("Generated content:\n%s", contentStr)
		}
		
		// Optional field should have omitempty
		if !strings.Contains(contentStr, `json:"optionalRef,omitempty"`) {
			t.Error("Optional reference field should have omitempty tag")
		}
		
		// Required field should not have omitempty
		if strings.Contains(contentStr, `json:"requiredRef,omitempty"`) {
			t.Error("Required reference field should not have omitempty tag")
		}
	})
}

func TestGenerator_NoRequiredFields(t *testing.T) {
	testDir := "testdata_no_required"
	schemaFile := filepath.Join(testDir, "test.json")
	outputFile := filepath.Join(testDir, "output.go")
	
	defer os.RemoveAll(testDir)
	
	// Schema with no required fields - all should have omitempty
	os.MkdirAll(testDir, 0755)
	os.WriteFile(schemaFile, []byte(`{
		"$defs": {
			"OptionalStruct": {
				"type": "object",
				"properties": {
					"field1": {"type": "string"},
					"field2": {"type": "integer"},
					"field3": {"type": ["string", "null"]}
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
	
	// Read and verify all fields have omitempty
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Errorf("Failed to read output file: %v", err)
		return
	}
	
	contentStr := string(content)
	
	// All fields should have omitempty since none are required
	if !strings.Contains(contentStr, `json:"field1,omitempty"`) {
		t.Error("Field1 should have omitempty when no required fields are specified")
	}
	if !strings.Contains(contentStr, `json:"field2,omitempty"`) {
		t.Error("Field2 should have omitempty when no required fields are specified")
	}
	if !strings.Contains(contentStr, `json:"field3,omitempty"`) {
		t.Error("Field3 should have omitempty when no required fields are specified")
	}
}
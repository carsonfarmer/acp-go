package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerator_GenerateWithIgnoreErrors(t *testing.T) {
	testDir := "testdata_ignore"
	// Schema with problematic definition that will cause error
	problemSchemaFile := filepath.Join(testDir, "problem.json")
	
	defer os.RemoveAll(testDir)
	
	// Setup test schema with empty struct (will cause "no properties found" error)
	os.MkdirAll(testDir, 0755)
	os.WriteFile(problemSchemaFile, []byte(`{
		"$defs": {
			"ValidType": {
				"type": "string",
				"description": "A valid type"
			},
			"EmptyStruct": {
				"type": "object",
				"description": "A struct with no properties"
			},
			"AnotherValidType": {
				"type": "integer",
				"description": "Another valid type"
			}
		}
	}`), 0644)

	tests := []struct {
		name         string
		ignoreErrors bool
		wantErr      bool
		errorMsg     string
	}{
		{
			name:         "without ignore errors - should fail",
			ignoreErrors: false,
			wantErr:      true,
			errorMsg:     "no properties found",
		},
		{
			name:         "with ignore errors - should succeed",
			ignoreErrors: true,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				InputFile:    problemSchemaFile,
				OutputFile:   "output.go",
				PackageName:  "test",
				IgnoreErrors: tt.ignoreErrors,
			}
			
			generator := NewGenerator(config)
			
			// Load schema
			err := generator.LoadSchema()
			if err != nil {
				t.Fatalf("Failed to load schema: %v", err)
			}
			
			// Test generation
			err = generator.Generate()
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Generator.Generate() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Generator.Generate() error = %v, want error containing %v", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Generator.Generate() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestGenerator_IgnoreErrorsOutput(t *testing.T) {
	testDir := "testdata_ignore2"
	schemaFile := filepath.Join(testDir, "mixed.json")
	outputFile := filepath.Join(testDir, "output.go")
	
	defer os.RemoveAll(testDir)
	
	// Schema with mix of valid and invalid definitions
	os.MkdirAll(testDir, 0755)
	os.WriteFile(schemaFile, []byte(`{
		"$defs": {
			"ValidEnum": {
				"type": "string",
				"oneOf": [
					{"const": "valid", "description": "Valid value"}
				]
			},
			"EmptyStruct": {
				"type": "object",
				"description": "Will cause error - no properties"
			},
			"ValidType": {
				"type": "string",
				"description": "Another valid type"
			}
		}
	}`), 0644)

	config := &Config{
		InputFile:    schemaFile,
		OutputFile:   outputFile,
		PackageName:  "test",
		IgnoreErrors: true,
	}
	
	// Validate config to create output directory
	err := config.Validate()
	if err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}
	
	generator := NewGenerator(config)
	
	// Load, generate and save with ignore errors
	err = generator.LoadSchema()
	if err != nil {
		t.Fatalf("Failed to load schema: %v", err)
	}
	
	err = generator.Generate()
	if err != nil {
		t.Errorf("Generator.Generate() with ignore errors should not fail: %v", err)
		return
	}
	
	err = generator.SaveToFile()
	if err != nil {
		t.Errorf("Generator.SaveToFile() error = %v", err)
		return
	}
	
	// Verify output contains valid types but not the problematic one
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Errorf("Failed to read output file: %v", err)
		return
	}
	
	contentStr := string(content)
	
	// Should contain valid types
	if !strings.Contains(contentStr, "ValidEnum") {
		t.Error("Output should contain ValidEnum type")
	}
	
	if !strings.Contains(contentStr, "ValidType") {
		t.Error("Output should contain ValidType")
	}
	
	// Should NOT contain the problematic struct (since it was skipped)
	if strings.Contains(contentStr, "EmptyStruct") {
		t.Error("Output should not contain EmptyStruct (should have been skipped)")
	}
}
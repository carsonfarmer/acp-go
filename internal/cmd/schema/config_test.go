package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				InputFile:   "testdata/schema.json",
				OutputFile:  "output/types.go",
				PackageName: "types",
			},
			wantErr: false,
		},
		{
			name: "empty input file",
			config: Config{
				InputFile:   "",
				OutputFile:  "output/types.go",
				PackageName: "types",
			},
			wantErr: true,
			errMsg:  "input file is required",
		},
		{
			name: "empty output file - should be valid (stdout)",
			config: Config{
				InputFile:   "testdata/schema.json",
				OutputFile:  "",
				PackageName: "types",
			},
			wantErr: false,
		},
		{
			name: "empty package name",
			config: Config{
				InputFile:   "testdata/schema.json",
				OutputFile:  "output/types.go",
				PackageName: "",
			},
			wantErr: true,
			errMsg:  "package name is required",
		},
		{
			name: "non-existent input file",
			config: Config{
				InputFile:   "non/existent/file.json",
				OutputFile:  "output/types.go",
				PackageName: "types",
			},
			wantErr: true,
			errMsg:  "input file does not exist",
		},
	}

	// Create test input file for valid tests
	testDir := "testdata"
	testFile := filepath.Join(testDir, "schema.json")
	
	// Clean up at the end
	defer func() {
		os.RemoveAll(testDir)
		os.RemoveAll("output")
	}()

	// Create testdata directory and file
	os.MkdirAll(testDir, 0755)
	os.WriteFile(testFile, []byte(`{"type": "object"}`), 0644)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Config.Validate() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if tt.errMsg != "" {
					// Check if error message contains expected substring
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("Config.Validate() error = %v, want error containing %v", err, tt.errMsg)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestNewConfig(t *testing.T) {
	config := NewConfig()
	
	if config == nil {
		t.Fatal("NewConfig() returned nil")
	}
	
	if config.PackageName != "main" {
		t.Errorf("NewConfig() PackageName = %v, want %v", config.PackageName, "main")
	}
	
	if config.InputFile != "" {
		t.Errorf("NewConfig() InputFile = %v, want empty string", config.InputFile)
	}
	
	if config.OutputFile != "" {
		t.Errorf("NewConfig() OutputFile = %v, want empty string", config.OutputFile)
	}
}
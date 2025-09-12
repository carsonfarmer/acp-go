package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds configuration for schema generation
type Config struct {
	InputFile    string   `yaml:"input"`     // Path to input JSON schema file
	MetaFile     string   `yaml:"meta"`      // Path to meta.json file (optional)
	OutputFile   string   `yaml:"output"`    // Path to output Go file  
	PackageName  string   `yaml:"package"`   // Go package name for generated code
	IgnoreErrors bool     `yaml:"ignoreErrors"` // Skip definitions that cause generation errors
	IgnoreTypes  []string `yaml:"ignoreTypes"` // List of type names to ignore during generation
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.InputFile == "" {
		return NewValidationError("input file is required", nil)
	}

	// OutputFile is optional - if empty, output goes to stdout

	if c.PackageName == "" {
		return NewValidationError("package name is required", nil)
	}

	// Check if input file exists
	if _, err := os.Stat(c.InputFile); os.IsNotExist(err) {
		return NewFileSystemError("input file does not exist", err).
			WithContext("inputFile", c.InputFile)
	}

	// Check if meta file exists (if specified)
	if c.MetaFile != "" {
		if _, err := os.Stat(c.MetaFile); os.IsNotExist(err) {
			return NewFileSystemError("meta file does not exist", err).
				WithContext("metaFile", c.MetaFile)
		}
	}

	// Create output directory if output file is specified
	if c.OutputFile != "" {
		outputDir := filepath.Dir(c.OutputFile)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return NewFileSystemError("failed to create output directory", err).
				WithContext("outputDir", outputDir)
		}
	}

	return nil
}

// NewConfig creates a new configuration with default values
func NewConfig() *Config {
	return &Config{
		PackageName: "main",
	}
}

// LoadConfigFromFile loads configuration from .schema.yaml file
func LoadConfigFromFile(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	// Resolve relative paths relative to config file directory
	configDir := filepath.Dir(configPath)
	if config.InputFile != "" && !filepath.IsAbs(config.InputFile) {
		config.InputFile = filepath.Join(configDir, config.InputFile)
	}
	if config.MetaFile != "" && !filepath.IsAbs(config.MetaFile) {
		config.MetaFile = filepath.Join(configDir, config.MetaFile)
	}
	if config.OutputFile != "" && !filepath.IsAbs(config.OutputFile) {
		config.OutputFile = filepath.Join(configDir, config.OutputFile)
	}

	return &config, nil
}

// FindSchemaConfig looks for .schema.yaml file in current directory and parent directories
func FindSchemaConfig() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	dir := currentDir
	for {
		configPath := filepath.Join(dir, ".schema.yaml")
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root directory
			break
		}
		dir = parent
	}

	return "", fmt.Errorf(".schema.yaml file not found")
}

// MergeWithFileConfig merges file config with CLI config, giving precedence to CLI flags
func (c *Config) MergeWithFileConfig(fileConfig *Config) {
	// Only use config file values if CLI flags are not set
	if c.InputFile == "" && fileConfig.InputFile != "" {
		c.InputFile = fileConfig.InputFile
	}
	if c.MetaFile == "" && fileConfig.MetaFile != "" {
		c.MetaFile = fileConfig.MetaFile
	}
	if c.OutputFile == "" && fileConfig.OutputFile != "" {
		c.OutputFile = fileConfig.OutputFile
	}
	if c.PackageName == "main" && fileConfig.PackageName != "" { // "main" is the default
		c.PackageName = fileConfig.PackageName
	}
	// For boolean flags, use config file value if CLI flag is not explicitly set (false is default)
	if !c.IgnoreErrors && fileConfig.IgnoreErrors {
		c.IgnoreErrors = fileConfig.IgnoreErrors
	}
	if len(c.IgnoreTypes) == 0 && len(fileConfig.IgnoreTypes) > 0 {
		c.IgnoreTypes = fileConfig.IgnoreTypes
	}
}
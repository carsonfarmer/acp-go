package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Target represents a single schema-to-Go generation target.
type Target struct {
	InputFile   string `yaml:"input"`       // Path to input JSON schema file
	MetaFile    string `yaml:"meta"`        // Path to meta.json file (optional)
	OutputFile  string `yaml:"output"`      // Path to output Go file
	ExcludeFrom         string `yaml:"excludeFrom"`         // Path to base schema; identical definitions are skipped
	ExcludeMetaFrom     string `yaml:"excludeMetaFrom"`     // Path to base meta; identical constants are skipped
	MergePropertiesFrom string `yaml:"mergePropertiesFrom"` // Path to extended schema; new properties on existing types are merged in
}

// Config holds configuration for schema generation
type Config struct {
	InputFile    string   `yaml:"input"`        // Path to input JSON schema file (single target, legacy)
	MetaFile     string   `yaml:"meta"`         // Path to meta.json file (single target, legacy)
	OutputFile   string   `yaml:"output"`       // Path to output Go file (single target, legacy)
	ExcludeFrom         string `yaml:"-"` // Path to base schema to exclude definitions from
	ExcludeMetaFrom     string `yaml:"-"` // Path to base meta to exclude constants from
	MergePropertiesFrom string `yaml:"-"` // Path to extended schema to merge new properties from
	PackageName     string `yaml:"package"` // Go package name for generated code
	IgnoreErrors bool     `yaml:"ignoreErrors"` // Skip definitions that cause generation errors
	IgnoreTypes  []string `yaml:"ignoreTypes"`  // List of type names to ignore during generation
	Targets      []Target `yaml:"targets"`      // Multiple generation targets
}

// GetTargets returns the list of targets to generate.
// If Targets is set, returns it. Otherwise, builds a single target from legacy fields.
func (c *Config) GetTargets() []Target {
	if len(c.Targets) > 0 {
		return c.Targets
	}
	return []Target{{
		InputFile:  c.InputFile,
		MetaFile:   c.MetaFile,
		OutputFile: c.OutputFile,
	}}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.PackageName == "" {
		return NewValidationError("package name is required", nil)
	}

	targets := c.GetTargets()
	if len(targets) == 0 {
		return NewValidationError("at least one target is required", nil)
	}

	for _, t := range targets {
		if t.InputFile == "" {
			return NewValidationError("input file is required", nil)
		}
		if _, err := os.Stat(t.InputFile); os.IsNotExist(err) {
			return NewFileSystemError("input file does not exist", err).
				WithContext("inputFile", t.InputFile)
		}
		if t.MetaFile != "" {
			if _, err := os.Stat(t.MetaFile); os.IsNotExist(err) {
				return NewFileSystemError("meta file does not exist", err).
					WithContext("metaFile", t.MetaFile)
			}
		}
		if t.OutputFile != "" {
			outputDir := filepath.Dir(t.OutputFile)
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return NewFileSystemError("failed to create output directory", err).
					WithContext("outputDir", outputDir)
			}
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
	for i := range config.Targets {
		if config.Targets[i].InputFile != "" && !filepath.IsAbs(config.Targets[i].InputFile) {
			config.Targets[i].InputFile = filepath.Join(configDir, config.Targets[i].InputFile)
		}
		if config.Targets[i].MetaFile != "" && !filepath.IsAbs(config.Targets[i].MetaFile) {
			config.Targets[i].MetaFile = filepath.Join(configDir, config.Targets[i].MetaFile)
		}
		if config.Targets[i].OutputFile != "" && !filepath.IsAbs(config.Targets[i].OutputFile) {
			config.Targets[i].OutputFile = filepath.Join(configDir, config.Targets[i].OutputFile)
		}
		if config.Targets[i].ExcludeFrom != "" && !filepath.IsAbs(config.Targets[i].ExcludeFrom) {
			config.Targets[i].ExcludeFrom = filepath.Join(configDir, config.Targets[i].ExcludeFrom)
		}
		if config.Targets[i].ExcludeMetaFrom != "" && !filepath.IsAbs(config.Targets[i].ExcludeMetaFrom) {
			config.Targets[i].ExcludeMetaFrom = filepath.Join(configDir, config.Targets[i].ExcludeMetaFrom)
		}
		if config.Targets[i].MergePropertiesFrom != "" && !filepath.IsAbs(config.Targets[i].MergePropertiesFrom) {
			config.Targets[i].MergePropertiesFrom = filepath.Join(configDir, config.Targets[i].MergePropertiesFrom)
		}
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
	if len(c.Targets) == 0 && len(fileConfig.Targets) > 0 {
		c.Targets = fileConfig.Targets
	}
}
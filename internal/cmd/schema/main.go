package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:      "schema-gen",
		Usage:     "Generate Go types from JSON schema",
		Version:   "1.0.0",
		// Authors field removed in v3
		Copyright: "MIT License",
		
		Commands: []*cli.Command{
			{
				Name:    "generate",
				Aliases: []string{"gen", "g"},
				Usage:   "Generate Go types from JSON schema (default command)",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "input",
						Aliases: []string{"i"},
						Usage:   "Input JSON schema file",
					},
					&cli.StringFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Usage:   "Output Go file (optional, defaults to stdout)",
					},
					&cli.StringFlag{
						Name:    "package",
						Aliases: []string{"p"},
						Usage:   "Go package name for generated code",
						Value:   "main",
					},
					&cli.BoolFlag{
						Name:    "ignore-errors",
						Usage:   "Skip definitions that cause generation errors",
						Value:   false,
					},
					&cli.StringFlag{
						Name:    "meta",
						Aliases: []string{"m"},
						Usage:   "Meta JSON file for generating constants and marking internal types",
					},
					&cli.StringSliceFlag{
						Name:    "ignore-types",
						Usage:   "Comma-separated list of type names to ignore during generation",
					},
					&cli.StringFlag{
						Name:    "config",
						Aliases: []string{"c"},
						Usage:   "Path to .schema.yaml config file (auto-detected if not specified)",
					},
				},
				Action: generateAction,
			},
			{
				Name:    "validate",
				Aliases: []string{"check", "v"},
				Usage:   "Validate JSON schema file without generating code",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "input",
						Aliases:  []string{"i"},
						Usage:    "Input JSON schema file to validate",
						Required: true,
					},
				},
				Action: validateAction,
			},
		},
		
		
		OnUsageError: func(ctx context.Context, cmd *cli.Command, err error, isSubcommand bool) error {
			// Don't print anything for nil errors
			if err == nil {
				return nil
			}
			
			if appErr, ok := err.(*AppError); ok {
				fmt.Fprintf(os.Stderr, "Error: %s\n", appErr.Message)
				if appErr.Type == ErrorTypeCLI {
					cli.ShowAppHelp(cmd)
				}
			} else {
				// Only print non-nil errors
				if err.Error() != "" && err.Error() != "<nil>" {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				}
			}
			return err
		},
	}
	
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// generateAction handles the main generate command
func generateAction(ctx context.Context, cmd *cli.Command) error {
	return executeGenerate(ctx, cmd)
}

// executeGenerate contains the common generation logic
func executeGenerate(_ context.Context, cmd *cli.Command) error {
	config := &Config{
		InputFile:    cmd.String("input"),
		MetaFile:     cmd.String("meta"),
		OutputFile:   cmd.String("output"),
		PackageName:  cmd.String("package"),
		IgnoreErrors: cmd.Bool("ignore-errors"),
		IgnoreTypes:  cmd.StringSlice("ignore-types"),
	}

	// Load schema config file if exists
	if err := loadSchemaConfigIfExists(config, cmd.String("config")); err != nil {
		fmt.Printf("Warning: Failed to load config file: %v\n", err)
	}

	if err := config.Validate(); err != nil {
		return err
	}

	targets := config.GetTargets()
	for _, target := range targets {
		targetConfig := &Config{
			InputFile:           target.InputFile,
			MetaFile:            target.MetaFile,
			OutputFile:          target.OutputFile,
			ExcludeFrom:         target.ExcludeFrom,
			ExcludeMetaFrom:     target.ExcludeMetaFrom,
			MergePropertiesFrom: target.MergePropertiesFrom,
			PackageName:         config.PackageName,
			IgnoreErrors:        config.IgnoreErrors,
			IgnoreTypes:         config.IgnoreTypes,
		}

		if err := generateTarget(targetConfig); err != nil {
			return err
		}
	}

	return nil
}

// generateTarget generates Go code for a single target.
func generateTarget(config *Config) error {
	generator := NewGenerator(config)

	// Load metadata if provided
	if err := generator.LoadMetadata(); err != nil {
		return NewGenerationError("failed to load metadata", err).
			WithContext("metaFile", config.MetaFile)
	}

	// Load schema
	if err := generator.LoadSchema(); err != nil {
		return NewGenerationError("failed to load schema", err).
			WithContext("inputFile", config.InputFile)
	}

	// Generate code
	if err := generator.Generate(); err != nil {
		return NewGenerationError("failed to generate code", err).
			WithContext("inputFile", config.InputFile)
	}

	// Save or output
	if config.OutputFile != "" {
		if err := generator.SaveToFile(); err != nil {
			return NewFileSystemError("failed to save file", err).
				WithContext("outputFile", config.OutputFile)
		}
		fmt.Printf("Successfully generated Go types from %s to %s\n", config.InputFile, config.OutputFile)
	} else {
		// Write to stdout
		content := generator.GetGeneratedContent()
		if _, err := os.Stdout.Write(content); err != nil {
			return NewSerializationError("failed to write output", err)
		}
		fmt.Fprintf(os.Stderr, "Successfully generated Go types from %s\n", config.InputFile)
	}

	// Print skipped items if any
	if generator.GetSkippedCount() > 0 {
		fmt.Printf("Skipped %d definitions: %v\n",
			generator.GetSkippedCount(), generator.GetSkippedItems())
	}

	return nil
}

// validateAction handles the validate command
func validateAction(ctx context.Context, cmd *cli.Command) error {
	inputFile := cmd.String("input")
	
	config := &Config{
		InputFile:   inputFile,
		PackageName: "temp", // Dummy package name for validation
	}

	if err := config.Validate(); err != nil {
		return err
	}

	generator := NewGenerator(config)
	if err := generator.LoadSchema(); err != nil {
		return NewValidationError("invalid JSON schema", err).
			WithContext("inputFile", inputFile)
	}

	fmt.Printf("✅ JSON schema file '%s' is valid\n", inputFile)
	return nil
}

// loadSchemaConfigIfExists loads .schema.yaml config file and merges with CLI config
func loadSchemaConfigIfExists(config *Config, configPath string) error {
	// If config path is not specified, try to find .schema.yaml
	if configPath == "" {
		foundPath, err := FindSchemaConfig()
		if err != nil {
			// No config file found, this is not an error
			return nil
		}
		configPath = foundPath
	}

	// Load the config file
	fileConfig, err := LoadConfigFromFile(configPath)
	if err != nil {
		return err
	}

	// Merge with CLI config (CLI flags take precedence)
	config.MergeWithFileConfig(fileConfig)
	return nil
}


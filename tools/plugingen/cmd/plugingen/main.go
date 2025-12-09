// plugingen is a command-line tool for generating type-safe Go code from plugin schemas.
//
// Usage:
//
//	plugingen generate -i schema.yaml -o generated.go
//	plugingen validate -i schema.yaml
//
// The tool reads a schema definition (YAML or JSON) and generates strongly-typed
// Go code with conversion functions between map[string]any and Go structs.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kart-io/goagent/tools/plugingen"
)

// Build-time variables (set via ldflags)
var (
	// Version is the semantic version of plugingen
	Version = "dev"

	// GitCommit is the git commit hash
	GitCommit = "unknown"

	// BuildDate is the build timestamp
	BuildDate = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "generate":
		runGenerate()
	case "validate":
		runValidate()
	case "version", "-v", "--version":
		printVersion()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

// printVersion prints detailed version information.
func printVersion() {
	fmt.Printf("plugingen %s\n", Version)
	fmt.Printf("  Git Commit: %s\n", GitCommit)
	fmt.Printf("  Build Date: %s\n", BuildDate)
	fmt.Printf("  Go Version: %s\n", runtime.Version())
	fmt.Printf("  OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// runGenerate executes the generate command.
func runGenerate() {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	inputFile := fs.String("i", "", "Input schema file (YAML or JSON)")
	outputFile := fs.String("o", "", "Output Go file")

	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *inputFile == "" {
		fmt.Fprintln(os.Stderr, "Error: -i (input file) is required")
		fs.Usage()
		os.Exit(1)
	}

	if *outputFile == "" {
		fmt.Fprintln(os.Stderr, "Error: -o (output file) is required")
		fs.Usage()
		os.Exit(1)
	}

	// Load schema
	fmt.Printf("Loading schema from %s...\n", *inputFile)
	schema, err := plugingen.LoadSchema(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading schema: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Schema loaded: %s v%s\n", schema.PluginName, schema.Version)

	// Create generator
	generator, err := plugingen.NewGenerator(schema)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating generator: %v\n", err)
		os.Exit(1)
	}

	// Generate code
	fmt.Println("Generating code...")
	code, err := generator.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating code: %v\n", err)
		os.Exit(1)
	}

	// Write output
	fmt.Printf("Writing output to %s...\n", *outputFile)
	if err := writeFile(*outputFile, code); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Successfully generated %s\n", *outputFile)
	fmt.Printf("  Package: %s\n", schema.PackageName)
	fmt.Printf("  Plugin: %s\n", schema.PluginName)
	fmt.Printf("  Input: %s\n", schema.InputType.Name)
	fmt.Printf("  Output: %s\n", schema.OutputType.Name)
}

// runValidate executes the validate command.
func runValidate() {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	inputFile := fs.String("i", "", "Input schema file (YAML or JSON)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *inputFile == "" {
		fmt.Fprintln(os.Stderr, "Error: -i (input file) is required")
		fs.Usage()
		os.Exit(1)
	}

	// Load and validate schema
	fmt.Printf("Validating schema from %s...\n", *inputFile)
	schema, err := plugingen.LoadSchema(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ Validation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Schema is valid!")
	fmt.Printf("  Package: %s\n", schema.PackageName)
	fmt.Printf("  Plugin: %s v%s\n", schema.PluginName, schema.Version)
	if schema.Description != "" {
		fmt.Printf("  Description: %s\n", schema.Description)
	}
	fmt.Printf("  Input: %s (%d fields)\n", schema.InputType.Name, len(schema.InputType.Fields))
	fmt.Printf("  Output: %s (%d fields)\n", schema.OutputType.Name, len(schema.OutputType.Fields))
}

// writeFile writes content to a file, creating directories if needed.
func writeFile(path, content string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// printUsage prints usage information.
func printUsage() {
	fmt.Printf(`plugingen %s - Type-safe plugin code generator

Usage:
  plugingen <command> [flags]

Commands:
  generate    Generate Go code from a schema
  validate    Validate a schema file
  version     Print version information
  help        Show this help message

Generate Command:
  plugingen generate -i <input> -o <output>

  Flags:
    -i string    Input schema file (YAML or JSON) [required]
    -o string    Output Go file [required]

  Example:
    plugingen generate -i plugin.yaml -o generated/plugin.go

Validate Command:
  plugingen validate -i <input>

  Flags:
    -i string    Input schema file (YAML or JSON) [required]

  Example:
    plugingen validate -i plugin.yaml

Schema Format:
  The schema file defines the plugin's input and output types.
  See examples/ directory for sample schemas.

  Supported formats: .yaml, .yml, .json

Documentation:
  https://github.com/kart-io/goagent/tools/plugingen

`, Version)
}

package main

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/ironpark/acp-go/internal/cmd/schema/facade"
	"github.com/ironpark/acp-go/internal/cmd/schema/tsdef"
	"github.com/ironpark/acp-go/internal/cmd/schema/tsgen"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	flags := flag.NewFlagSet("acp-schema", flag.ContinueOnError)
	source := flags.String("source", "schema/typescript", "Directory containing v1 and v2 TypeScript schema snapshots")
	output := flags.String("out", "schema", "Output directory for v1/*.gen.go and v2/*.gen.go")
	facadeRoot := flags.String("facade", "", "Module root to write the acp1 and acp2 façade files into; skipped when empty")
	check := flags.Bool("check", false, "Check generated files without writing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	// Complete parsing and generation of both versions before modifying outputs.
	type result struct {
		path string
		data []byte
	}
	var results []result
	specs := map[string]*facade.Spec{"v1": facade.V1, "v2": facade.V2}
	for _, version := range []string{"v1", "v2"} {
		schema, err := tsdef.ParseDir(filepath.Join(*source, version))
		if err != nil {
			return fmt.Errorf("%s: %w", version, err)
		}
		files, err := tsgen.Generate(schema, "schema")
		if err != nil {
			return fmt.Errorf("%s: %w", version, err)
		}
		for _, name := range slices.Sorted(maps.Keys(files)) {
			results = append(results, result{filepath.Join(*output, version, name), files[name]})
		}
		if *facadeRoot == "" {
			continue
		}
		spec := specs[version]
		facadeFiles, err := facade.Generate(spec, schema)
		if err != nil {
			return fmt.Errorf("%s façade: %w", version, err)
		}
		for _, name := range slices.Sorted(maps.Keys(facadeFiles)) {
			results = append(results, result{filepath.Join(*facadeRoot, spec.Dir, name), facadeFiles[name]})
		}
	}
	// Every *.gen.go in an output directory is ours: one the generator no
	// longer produces is stale, so -check reports it and a write removes it.
	produced := map[string]bool{}
	dirs := map[string]bool{}
	for _, r := range results {
		produced[r.path] = true
		dirs[filepath.Dir(r.path)] = true
	}
	var orphans []string
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		matches, err := filepath.Glob(filepath.Join(dir, "*.gen.go"))
		if err != nil {
			return err
		}
		for _, path := range matches {
			if !produced[path] {
				orphans = append(orphans, path)
			}
		}
	}
	for _, r := range results {
		if *check {
			existing, err := os.ReadFile(r.path)
			if err != nil {
				return err
			}
			if string(existing) != string(r.data) {
				return fmt.Errorf("%s is stale; run go generate ./...", r.path)
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(r.path), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(r.path, r.data, 0644); err != nil {
				return err
			}
		}
	}
	for _, path := range orphans {
		if *check {
			return fmt.Errorf("%s is no longer generated; run go generate ./...", path)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

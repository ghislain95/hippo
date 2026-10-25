// Command schemabundle assembles the classify_item tool schema and its fiche schemas into one
// self-contained JSON Schema (no external $ref), validates the fiche examples against it
// (docs/prompt/README.md, ADR-0006, ADR-0031), and emits the fiche schemas as OpenAPI
// components for the API contract (ADR-0035).
//
// Usage:
//
//	schemabundle -schemas <dir> -examples <dir> -out <file> -openapi-out <file>          write both
//	schemabundle -schemas <dir> -examples <dir> -out <file> -openapi-out <file> -check   fail if stale
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "schemabundle:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("schemabundle", flag.ContinueOnError)
	schemasDir := flags.String("schemas", "", "directory of classify-item.schema.json")
	examplesDir := flags.String("examples", "", "directory of the example classify_item answers")
	out := flags.String("out", "", "bundled schema file")
	openAPIOut := flags.String("openapi-out", "", "OpenAPI components file of the fiches")
	check := flags.Bool("check", false, "do not write: fail if the bundled file is not up to date")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *schemasDir == "" || *examplesDir == "" || *out == "" || *openAPIOut == "" {
		return errors.New("-schemas, -examples, -out and -openapi-out are required")
	}

	bundled, err := Bundle(*schemasDir, rootSchema)
	if err != nil {
		return err
	}
	count, err := ValidateExamples(bundled, *examplesDir)
	if err != nil {
		return err
	}
	components, err := OpenAPIComponents(*schemasDir)
	if err != nil {
		return err
	}

	outputs := []struct {
		path    string
		content []byte
	}{{*out, bundled}, {*openAPIOut, components}}
	for _, output := range outputs {
		if *check {
			current, err := os.ReadFile(output.path)
			if err != nil {
				return fmt.Errorf("read %s: %w", output.path, err)
			}
			if !bytes.Equal(current, output.content) {
				return fmt.Errorf("%s is out of date: run 'make generate'", output.path)
			}
			continue
		}
		if err := os.WriteFile(output.path, output.content, 0o644); err != nil { //nolint:gosec // a generated, versioned file
			return fmt.Errorf("write %s: %w", output.path, err)
		}
	}
	verb := "wrote"
	if *check {
		verb = "checked, up to date:"
	}
	fmt.Fprintf(stdout, "schemabundle: %s %s and %s, %d examples valid\n", verb, *out, *openAPIOut, count)
	return nil
}

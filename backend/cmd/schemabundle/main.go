// Command schemabundle assembles the classify_item tool schema and its fiche schemas into one
// self-contained JSON Schema (no external $ref), and validates the fiche examples against it
// (docs/prompt/README.md, ADR-0006, ADR-0031).
//
// Usage:
//
//	schemabundle -schemas <dir> -examples <dir> -out <file>          write the bundle
//	schemabundle -schemas <dir> -examples <dir> -out <file> -check   fail if it is stale
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
	check := flags.Bool("check", false, "do not write: fail if the bundled file is not up to date")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *schemasDir == "" || *examplesDir == "" || *out == "" {
		return errors.New("-schemas, -examples and -out are required")
	}

	bundled, err := Bundle(*schemasDir, rootSchema)
	if err != nil {
		return err
	}
	count, err := ValidateExamples(bundled, *examplesDir)
	if err != nil {
		return err
	}

	if *check {
		current, err := os.ReadFile(*out)
		if err != nil {
			return fmt.Errorf("read %s: %w", *out, err)
		}
		if !bytes.Equal(current, bundled) {
			return fmt.Errorf("%s is out of date: run 'make generate'", *out)
		}
		fmt.Fprintf(stdout, "schemabundle: %s up to date, %d examples valid\n", *out, count)
		return nil
	}
	if err := os.WriteFile(*out, bundled, 0o644); err != nil { //nolint:gosec // a generated, versioned file
		return fmt.Errorf("write %s: %w", *out, err)
	}
	fmt.Fprintf(stdout, "schemabundle: wrote %s, %d examples valid\n", *out, count)
	return nil
}

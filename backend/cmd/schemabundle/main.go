// Command schemabundle assembles the classify_item tool schema and its fiche schemas into one
// self-contained JSON Schema (no external $ref), validates the fiche examples against it
// (docs/prompt/README.md, ADR-0006, ADR-0031), and emits the fiche schemas as OpenAPI
// components for the API contract (ADR-0035). It also copies the fixed part and the version of
// the system prompt (docs/prompt/system-prompt.md) into a JSON file the backend embeds, since an
// embed directive cannot reach docs/ (design.md D4 of submit-nightly-batch).
//
// Usage:
//
//	schemabundle -schemas <dir> -examples <dir> -out <file> -openapi-out <file> -prompt <file> -prompt-out <file>          write all three
//	schemabundle -schemas <dir> -examples <dir> -out <file> -openapi-out <file> -prompt <file> -prompt-out <file> -check   fail if stale
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
	promptSource := flags.String("prompt", "", "system prompt (docs/prompt/system-prompt.md)")
	promptOut := flags.String("prompt-out", "", "generated copy of the fixed part and version of the prompt")
	check := flags.Bool("check", false, "do not write: fail if the bundled file is not up to date")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *schemasDir == "" || *examplesDir == "" || *out == "" || *openAPIOut == "" || *promptSource == "" || *promptOut == "" {
		return errors.New("-schemas, -examples, -out, -openapi-out, -prompt and -prompt-out are required")
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

	source, err := os.ReadFile(*promptSource)
	if err != nil {
		return fmt.Errorf("read %s: %w", *promptSource, err)
	}
	prompt, err := PromptCopy(source)
	if err != nil {
		return err
	}

	outputs := []struct {
		path    string
		content []byte
	}{{*out, bundled}, {*openAPIOut, components}, {*promptOut, prompt}}
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
	fmt.Fprintf(stdout, "schemabundle: %s %s, %s and %s, %d examples valid\n", verb, *out, *openAPIOut, *promptOut, count)
	return nil
}

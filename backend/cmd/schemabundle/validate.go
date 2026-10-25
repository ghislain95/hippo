package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidateExamples validates every *.json file of dir against the bundled schema, and returns
// how many were checked. Every fiche type must have an example: the examples are also the
// fixtures of the template and export tests (docs/dev/testing.md).
func ValidateExamples(bundled []byte, dir string) (int, error) {
	schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(bundled))
	if err != nil {
		return 0, fmt.Errorf("parse bundle: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("bundle.json", schemaDoc); err != nil {
		return 0, fmt.Errorf("load bundle: %w", err)
	}
	schema, err := compiler.Compile("bundle.json")
	if err != nil {
		return 0, fmt.Errorf("compile bundle: %w", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return 0, fmt.Errorf("list examples: %w", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return 0, fmt.Errorf("no example in %s", dir)
	}

	templates := map[string]bool{}
	for _, file := range files {
		raw, err := os.ReadFile(file) //nolint:gosec // a path listed from the examples directory
		if err != nil {
			return 0, fmt.Errorf("read example: %w", err)
		}
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			return 0, fmt.Errorf("parse %s: %w", file, err)
		}
		if err := schema.Validate(instance); err != nil {
			return 0, fmt.Errorf("%s does not match the schema: %w", filepath.Base(file), err)
		}
		var answer struct {
			Fiche struct {
				Template string `json:"template"`
			} `json:"fiche"`
		}
		if err := json.Unmarshal(raw, &answer); err != nil {
			return 0, fmt.Errorf("parse %s: %w", file, err)
		}
		templates[answer.Fiche.Template] = true
	}

	for _, template := range ficheTemplates(bundled) {
		if !templates[template] {
			return 0, fmt.Errorf("no example for the %q fiche: add %s/%s.json", template, dir, template)
		}
	}
	return len(files), nil
}

// ficheTemplates lists the template constants of the fiche schemas in the bundle.
func ficheTemplates(bundled []byte) []string {
	var doc struct {
		Defs map[string]struct {
			Properties struct {
				Template struct {
					Const string `json:"const"`
				} `json:"template"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(bundled, &doc); err != nil {
		return nil
	}
	var templates []string
	for _, def := range doc.Defs {
		if c := def.Properties.Template.Const; c != "" {
			templates = append(templates, c)
		}
	}
	sort.Strings(templates)
	return templates
}

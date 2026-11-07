// Package schema embeds the bundled classify_item tool schema: the input_schema of the
// analysis tool and the schema every AI answer is validated against (ADR-0006). Compile builds
// the validator once, at start-up, with the version of each fiche schema (RG-64).
package schema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// classify-item.bundled.json is generated from docs/prompt/schemas/ by cmd/schemabundle
// (ADR-0031). Never edit it: change the source schemas and run `make generate`.

// ClassifyItem is the self-contained JSON Schema of the classify_item tool input.
//
//go:embed classify-item.bundled.json
var ClassifyItem []byte

// ErrInvalid: an answer does not match the schema. The validator's own message is never kept:
// it quotes the values of the answer (apply-batch-results design D5).
var ErrInvalid = errors.New("the answer does not match the classify_item schema")

// ficheDefPrefix names the definitions of the fiche schemas in the bundle: fiche_<template>.
const ficheDefPrefix = "fiche_"

// Validator checks a classify_item answer against the bundled schema, compiled once.
type Validator struct {
	schema   *jsonschema.Schema
	versions map[string]int
}

// Compile compiles the embedded bundle and reads the x-schema-version of each fiche schema.
// An error means a broken build: the service does not start.
func Compile() (*Validator, error) {
	return compile(ClassifyItem)
}

func compile(bundled []byte) (*Validator, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(bundled))
	if err != nil {
		return nil, fmt.Errorf("compile the classify_item schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("classify-item.bundled.json", doc); err != nil {
		return nil, fmt.Errorf("compile the classify_item schema: %w", err)
	}
	compiled, err := compiler.Compile("classify-item.bundled.json")
	if err != nil {
		return nil, fmt.Errorf("compile the classify_item schema: %w", err)
	}
	versions, err := ficheVersions(bundled)
	if err != nil {
		return nil, fmt.Errorf("compile the classify_item schema: %w", err)
	}
	return &Validator{schema: compiled, versions: versions}, nil
}

// ficheVersions maps the template of each fiche schema of the bundle ($defs.fiche_<template>,
// whose template member is the constant <template>) to its x-schema-version.
func ficheVersions(bundled []byte) (map[string]int, error) {
	var doc struct {
		Defs map[string]struct {
			Version    *int `json:"x-schema-version"`
			Properties struct {
				Template struct {
					Const *string `json:"const"`
				} `json:"template"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(bundled, &doc); err != nil {
		return nil, fmt.Errorf("read the fiche versions: %w", err)
	}
	versions := map[string]int{}
	for name, def := range doc.Defs {
		if !strings.HasPrefix(name, ficheDefPrefix) {
			continue
		}
		template := strings.TrimPrefix(name, ficheDefPrefix)
		if c := def.Properties.Template.Const; c == nil || *c != template {
			return nil, fmt.Errorf("$defs.%s: its template is not the constant %q", name, template)
		}
		if def.Version == nil || *def.Version < 1 {
			return nil, fmt.Errorf("$defs.%s: no x-schema-version", name)
		}
		versions[template] = *def.Version
	}
	if len(versions) == 0 {
		return nil, errors.New("no fiche schema in the bundle")
	}
	return versions, nil
}

// Validate checks instance, a JSON value decoded with json.Decoder.UseNumber (maps, slices,
// strings, json.Number, booleans, nil), against the schema. It answers ErrInvalid, never the
// validator's message.
func (v *Validator) Validate(instance any) error {
	if err := v.schema.Validate(instance); err != nil {
		return ErrInvalid
	}
	return nil
}

// FicheVersion returns the x-schema-version of the fiche schema of template (RG-64).
func (v *Validator) FicheVersion(template string) (int, bool) {
	version, ok := v.versions[template]
	return version, ok
}

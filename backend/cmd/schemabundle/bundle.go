package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// rootSchema is the entry point of the bundle, relative to the schemas directory.
const rootSchema = "classify-item.schema.json"

// bundler inlines every schema reachable from the root. An external reference to a whole file
// ("fiches/tech_article.schema.json") becomes "#/$defs/fiche_tech_article"; a reference to one of
// its definitions ("_common.schema.json#/$defs/text") becomes "#/$defs/common_text". Definition
// names are therefore flat and stable.
type bundler struct {
	dir    string
	defs   map[string]any
	loaded map[string]bool
}

// Bundle returns the self-contained schema, as indented JSON ending with a newline.
func Bundle(dir, root string) ([]byte, error) {
	b := &bundler{dir: dir, defs: map[string]any{}, loaded: map[string]bool{}}
	doc, err := b.read(root)
	if err != nil {
		return nil, err
	}
	if err := b.hoistDefs(root, doc); err != nil {
		return nil, err
	}
	resolved, err := b.resolve(root, doc)
	if err != nil {
		return nil, err
	}
	rootObject, ok := resolved.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: the root schema is not an object", root)
	}
	if len(b.defs) > 0 {
		rootObject["$defs"] = b.defs
	}
	delete(rootObject, "$id")

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(rootObject); err != nil {
		return nil, fmt.Errorf("encode bundle: %w", err)
	}
	if strings.Contains(buf.String(), `.schema.json`) {
		return nil, fmt.Errorf("bundle still references an external schema file")
	}
	return buf.Bytes(), nil
}

func (b *bundler) read(file string) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(b.dir, filepath.FromSlash(file)))
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	return doc, nil
}

// fileKey names a schema file in the flat $defs: "fiches/recipe.schema.json" → "fiche_recipe",
// "fiches/_common.schema.json" → "common".
func fileKey(file string) string {
	base := strings.TrimSuffix(path.Base(file), ".schema.json")
	if strings.HasPrefix(base, "_") {
		return strings.TrimPrefix(base, "_")
	}
	if path.Dir(file) == "fiches" {
		return "fiche_" + base
	}
	return base
}

// load inlines a whole file once, under its file key, and hoists its own definitions.
func (b *bundler) load(file string) error {
	if b.loaded[file] {
		return nil
	}
	b.loaded[file] = true
	doc, err := b.read(file)
	if err != nil {
		return err
	}
	if err := b.hoistDefs(file, doc); err != nil {
		return err
	}
	delete(doc, "$schema")
	delete(doc, "$id")
	if len(doc) == 0 {
		return nil
	}
	if _, onlyDefs := doc["title"]; onlyDefs && len(doc) == 1 {
		return nil // a file of definitions only, like _common.schema.json
	}
	resolved, err := b.resolve(file, doc)
	if err != nil {
		return err
	}
	b.defs[fileKey(file)] = resolved
	return nil
}

func (b *bundler) hoistDefs(file string, doc map[string]any) error {
	rawDefs, ok := doc["$defs"]
	if !ok {
		return nil
	}
	delete(doc, "$defs")
	defs, ok := rawDefs.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: $defs is not an object", file)
	}
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		resolved, err := b.resolve(file, defs[name])
		if err != nil {
			return err
		}
		b.defs[fileKey(file)+"_"+name] = resolved
	}
	return nil
}

// resolve rewrites every $ref of node, written in file, to a reference inside the bundle.
func (b *bundler) resolve(file string, node any) (any, error) {
	switch value := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			if key == "$ref" {
				ref, ok := child.(string)
				if !ok {
					return nil, fmt.Errorf("%s: $ref is not a string", file)
				}
				rewritten, err := b.rewrite(file, ref)
				if err != nil {
					return nil, err
				}
				out[key] = rewritten
				continue
			}
			resolved, err := b.resolve(file, child)
			if err != nil {
				return nil, err
			}
			out[key] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			resolved, err := b.resolve(file, child)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	default:
		return node, nil
	}
}

func (b *bundler) rewrite(file, ref string) (string, error) {
	target, fragment, _ := strings.Cut(ref, "#")
	targetFile := file
	if target != "" {
		targetFile = path.Join(path.Dir(file), target)
		if err := b.load(targetFile); err != nil {
			return "", err
		}
	}
	switch {
	case fragment == "":
		return "#/$defs/" + fileKey(targetFile), nil
	case strings.HasPrefix(fragment, "/$defs/") && !strings.Contains(strings.TrimPrefix(fragment, "/$defs/"), "/"):
		return "#/$defs/" + fileKey(targetFile) + "_" + strings.TrimPrefix(fragment, "/$defs/"), nil
	default:
		return "", fmt.Errorf("%s: unsupported $ref %q (only a whole file or #/$defs/<name>)", file, ref)
	}
}

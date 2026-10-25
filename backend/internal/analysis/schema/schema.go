// Package schema embeds the bundled classify_item tool schema: the input_schema of the
// analysis tool and the schema every AI answer is validated against (ADR-0006).
package schema

import _ "embed"

// classify-item.bundled.json is generated from docs/prompt/schemas/ by cmd/schemabundle
// (ADR-0031). Never edit it: change the source schemas and run `make generate`.

// ClassifyItem is the self-contained JSON Schema of the classify_item tool input.
//
//go:embed classify-item.bundled.json
var ClassifyItem []byte

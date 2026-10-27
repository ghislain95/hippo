package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
)

const bundledContract = "../../../api/openapi.bundled.gen.json"

// requestBodies holds one valid JSON body per operation whose request body is required. A new
// operation with a required body and no entry here fails the test, so that it gets a case.
var requestBodies = map[string]string{
	"updateItem":            `{"is_pinned": true}`,
	"updateReadingProgress": `{"read_progress": 0.5}`,
	"createCategory":        `{"name": "Kubernetes"}`,
	"updateCategory":        `{"name": "Orchestration"}`,
	"mergeCategory":         `{"target_id": 2}`,
	"updateContentType":     `{"name": "Évènement"}`,
	"mergeContentType":      `{"target_key": "note"}`,
	"updateTag":             `{"name": "go"}`,
	"mergeTag":              `{"target_id": 2}`,
}

// pathValues gives a value of the right type to every path parameter of the contract.
var pathValues = map[string]string{
	"{item_id}":     "01JB8Z6Q4M1X2Y3Z4A5B6C7D8E",
	"{asset_id}":    "01JB8Z6Q4M1X2Y3Z4A5B6C7D8F",
	"{category_id}": "1",
	"{tag_id}":      "1",
	"{type_key}":    "recipe",
}

// implementedOperations answer for real: the sweep skips them.
var implementedOperations = map[string]bool{
	"getHealth":  true,
	"createItem": true, // capture-link (E2-01), capture-image (E2-04)
	"getAsset":   true, // capture-image (E2-04)
}

type contractOperation struct {
	OperationID string `json:"operationId"`
	RequestBody *struct {
		Required bool `json:"required"`
	} `json:"requestBody"`
}

// TestEveryOperationAnswers501 is the scenario "Every declared operation answers 501 until
// implemented" of the api-conventions spec. It reads the bundled contract, so that every
// operation added to api/openapi.yaml is covered as soon as it is generated.
func TestEveryOperationAnswers501(t *testing.T) {
	raw, err := os.ReadFile(bundledContract)
	if err != nil {
		t.Fatalf("read bundled contract: %v", err)
	}
	var contract struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatalf("parse bundled contract: %v", err)
	}

	paths := make([]string, 0, len(contract.Paths))
	for path := range contract.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	t.Run("Every declared operation answers 501 until implemented", func(t *testing.T) {
		checkEveryOperation(t, contract.Paths, paths)
	})
}

func checkEveryOperation(t *testing.T, contractPaths map[string]map[string]json.RawMessage, paths []string) {
	t.Helper()
	f := newFixture(t)
	count := 0
	for _, path := range paths {
		for method, rawOperation := range contractPaths[path] {
			if method == "parameters" {
				continue
			}
			var operation contractOperation
			if err := json.Unmarshal(rawOperation, &operation); err != nil {
				t.Fatalf("parse %s %s: %v", method, path, err)
			}
			if implementedOperations[operation.OperationID] {
				continue
			}
			count++
			t.Run(operation.OperationID, func(t *testing.T) {
				url := path
				for placeholder, value := range pathValues {
					url = strings.ReplaceAll(url, placeholder, value)
				}
				if strings.Contains(url, "{") {
					t.Fatalf("%s: no value for a path parameter of %s; add it to pathValues", operation.OperationID, path)
				}
				body, contentType := "", ""
				if operation.RequestBody != nil && operation.RequestBody.Required {
					var ok bool
					if body, ok = requestBodies[operation.OperationID]; !ok {
						t.Fatalf("%s has a required body and no case in requestBodies", operation.OperationID)
					}
					contentType = "application/json"
				}
				resp := f.do(t, strings.ToUpper(method), url, testToken, contentType, body)
				problem := assertProblem(t, resp, http.StatusNotImplemented)
				if problem.Type != notImplementedType {
					t.Errorf("type = %q, want %s", problem.Type, notImplementedType)
				}
			})
		}
	}
	// The V1 contract declares 26 operations; each implemented one leaves the sweep.
	if want := 26 - len(implementedOperations); count < want {
		t.Errorf("checked %d operations, want %d at least", count, want)
	}
}

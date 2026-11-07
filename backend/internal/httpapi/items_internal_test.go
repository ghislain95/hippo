package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// TestItemFromStore covers the stored values the contract cannot answer (apply-batch-results
// design D13): no store function writes them, so they are built here.
func TestItemFromStore(t *testing.T) {
	stored := func() store.Item {
		return store.Item{
			ID: "01JB8Z6Q4M1X2Y3Z4A5B6C7D8E", Kind: "link", Source: "share", Title: "t", Status: "analyzed",
			ReadStatus: "unread", CreatedAt: time.Unix(0, 0), UpdatedAt: time.Unix(0, 0),
		}
	}

	t.Run("a stored fiche that does not decode is an internal error naming no value", func(t *testing.T) {
		for name, fiche := range map[string]string{
			"not JSON":         `{"template":"tip","tip":"FICHE-VALUE-SECRET`,
			"unknown template": `{"template":"book","title":"FICHE-VALUE-SECRET"}`,
			"wrong member":     `{"template":"tip","tip":["FICHE-VALUE-SECRET"]}`,
		} {
			item := stored()
			item.FicheJSON = json.RawMessage(fiche)
			_, err := itemFromStore(item)
			if !errors.Is(err, errStoredItem) {
				t.Errorf("%s: itemFromStore = %v, want errStoredItem", name, err)
			}
			if err != nil && strings.Contains(err.Error(), "FICHE-VALUE-SECRET") {
				t.Errorf("%s: the error %q quotes the fiche", name, err)
			}
		}
	})

	t.Run("a stored code outside AnalysisError is an internal error", func(t *testing.T) {
		item := stored()
		code := "quota_exceeded"
		item.LastError = &code
		if _, err := itemFromStore(item); !errors.Is(err, errStoredItem) {
			t.Errorf("itemFromStore = %v, want errStoredItem", err)
		}
	})

	t.Run("an item without fiche nor tags answers no fiche and an empty array", func(t *testing.T) {
		got, err := itemFromStore(stored())
		if err != nil || got.Fiche != nil || got.TagIds == nil || len(got.TagIds) != 0 {
			t.Errorf("itemFromStore = fiche %v, tags %#v, %v; want no fiche and []", got.Fiche, got.TagIds, err)
		}
	})
}

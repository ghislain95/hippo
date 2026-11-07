// Package analysis sends the waiting items to the AI, which classifies each one and writes its
// fiche (RG-30 to RG-37, ADR-0005, ADR-0006).
//
// This file and its neighbours build the requests of a nightly batch, in the Message Batches
// format, without the SDK: the request bytes are Hippo's, testable and streamed to disk
// (design.md D4 to D6 of submit-nightly-batch):
//
//   - the référentiel, the user's types, categories and tags as the AI sees them (Referentiel);
//   - the item message, an <element> block describing the item by its form, then its content or
//     its image (RG-23, RG-24, RG-26);
//   - the request writer (Requests), which appends one request per item to an unnamed file of
//     DATA_DIR/tmp, streams the image copies in base64 without holding them, keeps the body
//     under its byte limit (RG-32), refuses a private item (RG-12), and assembles the body of
//     the items the submission actually moved.
//
// The nightly pass (Pass, design.md D3, D7, D8) runs, for each user: the reconciliation of
// the batches whose send has an unknown outcome, the guards in their order (an API key, no
// batch without a recorded cost, the Paris month's spend below the budget: RG-81, ADR-0021),
// the selection, the requests, the submission transaction that re-reads each item (RG-12), and
// one send through the Analyzer, never retried. The Anthropic SDK hides behind Analyzer, in
// internal/analysis/anthropic (ADR-0005); tests use analysistest.
//
// The results of a batch (results.go, apply-batch-results design D1 to D8) are read one line
// at a time once the service reports the batch ended: each response is validated in a fixed
// order (stop reason, one classify_item call, tags normalised, the bundled schema, then the
// type, template and category in the store's transaction) and rejected whole on the first
// failure (RG-35); each line is settled in its own transaction, only while its item is still
// submitted in that batch; the batch is closed once, with the tokens of every succeeded line
// and their cost at the batch price (BatchCost, RG-80). Reading a batch builds no request.
//
// No item content reaches a log nor an error: only ids, counts and reason classes (RG-92,
// ADR-0027).
package analysis

import (
	"context"
	"errors"
	"os"

	"github.com/ghislain95/hippo/backend/internal/imagecopy"
	"github.com/ghislain95/hippo/backend/internal/store"
)

const (
	// MaxBatchItems is the number of requests of a batch at most (RG-32).
	MaxBatchItems = 200
	// MaxBodySize bounds the body of a batch, in bytes: 200 MiB, under the API's limit (RG-32,
	// design.md D2).
	MaxBodySize int64 = 200 << 20
	// maxTokens is the output cap of a request; maxTokensRetried that of an item whose response
	// was already cut once (RG-36).
	maxTokens        = 4000
	maxTokensRetried = 8000
	// toolName is the name of the only tool of a request, forced by tool_choice (ADR-0006).
	toolName = "classify_item"
)

var (
	// ErrPrivateItem: a private item reached the request building. The selection never returns
	// one; the user's batch stops and nothing is sent, so that a bug blocks the analysis rather
	// than leaks the item (RG-12, ADR-0023, design.md D11).
	ErrPrivateItem = errors.New("a private item reached the request building")
	// ErrItemGone: the item was deleted since the selection; it is skipped, the batch goes on.
	ErrItemGone = errors.New("the item is gone since the selection")
	// ErrCopierUnavailable: the child that copies an image could not start. The NAS is at fault,
	// not the image: the user's batch is not sent (RG-37, design.md D10). It wraps
	// imagecopy.ErrUnavailable.
	ErrCopierUnavailable = errors.New("image copier unavailable")
	// errStorage: a temporary file of the batch cannot be written or read (full disk). The
	// user's batch is not sent (storage_unavailable).
	errStorage = errors.New("temporary file of the batch unavailable")
	// errBodyFull: the request does not fit under the body limit.
	errBodyFull = errors.New("body limit reached")
)

// ContentReader reads the extracted content of one item, so that one content at a time is in
// memory (store.Store).
type ContentReader interface {
	ItemContent(ctx context.Context, userID, id string) (*string, error)
}

// AssetOpener opens a stored image read-only, from its path relative to the directory of the
// images (assetfs.Dir, which analysis does not import).
type AssetOpener interface {
	Open(rel string) (*os.File, error)
}

// TempFiles creates an unnamed temporary file of DATA_DIR/tmp: its write end (read-write) and
// a read-only end (extraction.TempDir).
type TempFiles interface {
	NewFile() (w, r *os.File, err error)
}

// ImageCopier copies a stored image to out, in a bounded child (imagecopy.Copier).
type ImageCopier interface {
	Copy(ctx context.Context, asset, out *os.File) (imagecopy.Result, error)
}

// Catalog reads the types, the categories and the tags of the référentiel (store.Store).
type Catalog interface {
	ContentTypes(ctx context.Context) ([]store.ContentType, error)
	Categories(ctx context.Context, userID string) ([]store.Category, error)
	ReferentielTags(ctx context.Context, userID string, limit int) ([]string, error)
}

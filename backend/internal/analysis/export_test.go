package analysis

import (
	"context"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// SegmentFileSize is the size of the segment file of r: the requests kept, nothing more.
func SegmentFileSize(r *Requests) (int64, error) {
	info, err := r.segW.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// WithSelected makes the pass hand the request building the queue selected returns instead of
// the selection, as if the selection had let it through (RG-12 tests).
func WithSelected(cfg *PassConfig, selected func(userID string, queue []store.AnalysisQueueItem) []store.AnalysisQueueItem) {
	cfg.selected = selected
}

// WithBeforeSubmit runs hook after the requests are written and before the submission
// transaction (RG-12 tests).
func WithBeforeSubmit(cfg *PassConfig, hook func(ctx context.Context, userID string, ids []string)) {
	cfg.beforeSubmit = hook
}

// Match is the decision of the reconciliation for one pending batch.
type Match = match

// MatchPending is the matching rule of the reconciliation (design.md D8).
func MatchPending(pending []store.Batch, listed []BatchInfo, recorded map[string]bool) []Match {
	return matchPending(pending, listed, recorded)
}

// WithAfterSubmit runs hook right after the submission transaction, before the body is sent
// (RG-12 and D7 tests).
func WithAfterSubmit(cfg *PassConfig, hook func(ctx context.Context, userID, batchID string)) {
	cfg.afterSubmit = hook
}

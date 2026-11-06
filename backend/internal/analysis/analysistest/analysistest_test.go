package analysistest_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/analysistest"
)

const twoRequests = `{"requests":[{"custom_id":"a","params":{}},{"custom_id":"b","params":{}}]}`

func create(t *testing.T, a *analysistest.Analyzer, body string) (analysis.BatchRef, error) {
	t.Helper()
	return a.CreateBatch(context.Background(), bytes.NewReader([]byte(body)), int64(len(body)))
}

func TestAnalyzer(t *testing.T) {
	now := time.Date(2027, 1, 10, 1, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	t.Run("records each body and accepts by default", func(t *testing.T) {
		a := analysistest.New(clock)
		ref, err := create(t, a, twoRequests)
		if err != nil || ref.ID == "" || !ref.CreatedAt.Equal(now) {
			t.Fatalf("CreateBatch = %+v, %v", ref, err)
		}
		bodies := a.Bodies()
		if len(bodies) != 1 || string(bodies[0].Raw) != twoRequests || len(bodies[0].IDs()) != 2 || bodies[0].IDs()[1] != "b" {
			t.Errorf("bodies = %+v", bodies)
		}
		listed, err := a.ListBatches(context.Background(), now.Add(-time.Hour))
		if err != nil || len(listed) != 1 || listed[0].ID != ref.ID || listed[0].RequestCount != 2 {
			t.Errorf("ListBatches = %+v, %v", listed, err)
		}
	})

	t.Run("plays the scripted outcomes in order", func(t *testing.T) {
		a := analysistest.New(clock)
		a.Script(analysistest.Reject, analysistest.Unknown, analysistest.AcceptLost)
		if _, err := create(t, a, twoRequests); !errors.Is(err, analysis.ErrRejected) {
			t.Errorf("first create = %v, want rejected", err)
		}
		if _, err := create(t, a, twoRequests); !errors.Is(err, analysistest.ErrLost) || errors.Is(err, analysis.ErrRejected) {
			t.Errorf("second create = %v, want an unknown outcome", err)
		}
		if _, err := create(t, a, twoRequests); !errors.Is(err, analysistest.ErrLost) {
			t.Errorf("third create = %v, want an unknown outcome", err)
		}
		if _, err := create(t, a, twoRequests); err != nil {
			t.Errorf("fourth create = %v, want accepted", err)
		}
		listed, err := a.ListBatches(context.Background(), time.Time{})
		if err != nil || len(listed) != 2 {
			t.Errorf("ListBatches = %+v, %v; want the lost and the accepted batches", listed, err)
		}
		if a.Creates() != 4 || a.Lists() != 1 || a.Calls() != 5 {
			t.Errorf("calls = %d creates, %d lists", a.Creates(), a.Lists())
		}
	})

	t.Run("lists the held batches newest first, without those before since", func(t *testing.T) {
		a := analysistest.New(clock)
		a.Hold(analysis.BatchInfo{ID: "old", CreatedAt: now.Add(-2 * time.Hour), RequestCount: 1})
		a.Hold(analysis.BatchInfo{ID: "mid", CreatedAt: now.Add(-time.Hour), RequestCount: 1})
		a.Hold(analysis.BatchInfo{ID: "new", CreatedAt: now, RequestCount: 1})
		listed, err := a.ListBatches(context.Background(), now.Add(-time.Hour))
		if err != nil || len(listed) != 2 || listed[0].ID != "new" || listed[1].ID != "mid" {
			t.Errorf("ListBatches = %+v, %v", listed, err)
		}
	})

	t.Run("plays a server error as an unknown outcome", func(t *testing.T) {
		a := analysistest.New(clock)
		a.Script(analysistest.ServerFail)
		_, err := create(t, a, twoRequests)
		var server *analysis.ServerError
		if errors.Is(err, analysis.ErrRejected) || !errors.As(err, &server) || server.Status != 503 {
			t.Errorf("CreateBatch = %v, want a server error", err)
		}
	})

	t.Run("runs its hooks and blocks the lists when told to", func(t *testing.T) {
		a := analysistest.New(clock)
		var creates, lists int
		a.OnCreate(func(context.Context) { creates++ })
		a.OnList(func(context.Context) { lists++ })
		a.BlockLists()
		if _, err := create(t, a, twoRequests); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if _, err := a.ListBatches(ctx, now); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("ListBatches = %v, want the deadline", err)
		}
		if creates != 1 || lists != 1 {
			t.Errorf("hooks ran %d and %d times, want 1 and 1", creates, lists)
		}
	})

	t.Run("fails the lists when told to", func(t *testing.T) {
		a := analysistest.New(clock)
		a.FailLists(true)
		if _, err := a.ListBatches(context.Background(), now); !errors.Is(err, analysistest.ErrList) {
			t.Errorf("ListBatches = %v, want ErrList", err)
		}
	})

	t.Run("refuses a body that is not JSON or not of its size", func(t *testing.T) {
		a := analysistest.New(clock)
		if _, err := create(t, a, "not json"); !errors.Is(err, analysis.ErrRejected) {
			t.Errorf("not JSON = %v, want rejected", err)
		}
		if _, err := a.CreateBatch(context.Background(), bytes.NewReader([]byte(twoRequests)), 3); !errors.Is(err, analysis.ErrRejected) {
			t.Errorf("wrong size = %v, want rejected", err)
		}
		if len(a.Bodies()) != 2 {
			t.Errorf("%d bodies recorded, want 2", len(a.Bodies()))
		}
	})
}

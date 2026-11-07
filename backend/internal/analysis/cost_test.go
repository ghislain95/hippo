package analysis_test

import (
	"math"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/analysis"
)

// haiku are the default prices, Claude Haiku 4.5's.
var haiku = analysis.Prices{InputPerMTok: 1, OutputPerMTok: 5}

// sameCost tells whether two costs are equal to a millionth of a cent.
func sameCost(a, b float64) bool { return math.Abs(a-b) < 1e-8 }

func TestBatchCost(t *testing.T) {
	t.Run("The cost follows the batch price", func(t *testing.T) {
		got := analysis.BatchCost(analysis.Usage{Input: 10000, Output: 2000, CacheWrite5m: 4000, CacheRead: 36000}, haiku)
		if !sameCost(got, 0.0143) {
			t.Errorf("BatchCost = %v, want 0.0143", got)
		}
	})

	t.Run("A one-hour cache write costs twice the input price", func(t *testing.T) {
		if got := analysis.BatchCost(analysis.Usage{CacheWrite1h: 1000}, haiku); !sameCost(got, 0.001) {
			t.Errorf("BatchCost = %v, want 0.001", got)
		}
	})

	t.Run("no token costs nothing", func(t *testing.T) {
		if got := analysis.BatchCost(analysis.Usage{}, haiku); got != 0 {
			t.Errorf("BatchCost = %v, want 0", got)
		}
	})
}

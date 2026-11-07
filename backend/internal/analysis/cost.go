package analysis

// Prices are the standard prices of the analysis model, in US dollars per million tokens
// (config.Prices, ANALYSIS_INPUT_PRICE_USD_PER_MTOK and ANALYSIS_OUTPUT_PRICE_USD_PER_MTOK).
type Prices struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// Ratios of the batch price (RG-80, ADR-0021, design D12 of apply-batch-results, checked on
// 2026-11-07): the Batch API charges half the standard prices, and the cache multipliers stack
// with that discount.
const (
	batchDiscount     = 0.5
	cacheWrite5mRatio = 1.25
	cacheWrite1hRatio = 2
	cacheReadRatio    = 0.1
)

// BatchCost is the cost in US dollars of the tokens u at the batch price of p: half of the
// input price times input tokens, 1.25 times it per 5-minute cache-write token, 2 times per
// 1-hour one, 0.1 times per cache-read token, plus the output price times output tokens.
func BatchCost(u Usage, p Prices) float64 {
	input := float64(u.Input) + cacheWrite5mRatio*float64(u.CacheWrite5m) + cacheWrite1hRatio*float64(u.CacheWrite1h) +
		cacheReadRatio*float64(u.CacheRead)
	return batchDiscount * (input*p.InputPerMTok + float64(u.Output)*p.OutputPerMTok) / 1e6
}

package context

// CostEstimator estimates model-context cost without requiring a tokenizer.
type CostEstimator interface {
	Tokens(text string) int
}

// ByteQuarterEstimator is deterministic and intentionally labeled an estimate.
type ByteQuarterEstimator struct{}

func (ByteQuarterEstimator) Tokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return (len([]byte(text)) + 3) / 4
}

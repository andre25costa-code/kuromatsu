package utils

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

// WithK1 overrides the term-frequency saturation constant (default 1.2).
func WithK1(k1 float64) BM25Option {
	return func(c *bm25Config) { c.k1 = k1 }
}

// WithB overrides the document-length normalization factor (default 0.75).
func WithB(b float64) BM25Option {
	return func(c *bm25Config) { c.b = b }
}

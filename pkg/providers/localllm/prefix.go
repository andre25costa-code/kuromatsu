// Package localllm -- prefix.go has no build tag (unlike engine_cgo.go /
// engine_stub.go) so commonPrefixLen is plain, cgo-free Go: testable on any
// machine, including this one (Windows, no C toolchain -- see SOUL.md).
package localllm

// commonPrefixLen returns the length of the longest common prefix of a and
// b, comparing element by element. Used by the cgo engine (B1/ADR-015/S21)
// to find out how much of the KV cache's currently-decoded tokens (kvTokens)
// can be reused for a new prompt's tokens: only the tokens that still match,
// in order, from index 0, are still valid once the prompt diverges.
func commonPrefixLen[T comparable](a, b []T) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// Package refinery is the native port of the context-refinery pipeline
// (Python-data-and-prompts/) for the Kuromatsu binary -- ADR-020. It holds
// only deterministic, allocation-light pieces: text cleanup, 64-bit SimHash
// fingerprints and the atom store. Anything that needs a generative model
// goes through the external model of ADR-018, never the native one (BR-008).
package refinery

import (
	"hash/fnv"
	"math/bits"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// SimHash64 is Charikar's SimHash over character 3..5-grams of the
// normalized text (NFC, lowercased, punctuation and whitespace collapsed). The Hamming distance
// between two fingerprints approximates the angle between their n-gram
// vectors: a LEXICAL similarity, not a semantic one -- paraphrases and
// PT<->EN pairs are not detected (ADR-020 §2). Used only for near-duplicate
// dedupe and redundancy penalties.
func SimHash64(text string) uint64 {
	runes := []rune(normalizeForFingerprint(text))
	h := fnv.New64a()
	if len(runes) > 0 && len(runes) < 3 {
		// Too short for a 3-gram: without this every short text would get
		// fingerprint 0 and collide with every other one. Hash it whole, so
		// only identical (normalized) short texts match.
		_, _ = h.Write([]byte(string(runes)))
		return h.Sum64()
	}
	var votes [64]int
	for n := 3; n <= 5; n++ {
		for i := 0; i+n <= len(runes); i++ {
			h.Reset()
			_, _ = h.Write([]byte(string(runes[i : i+n])))
			sum := h.Sum64()
			for b := 0; b < 64; b++ {
				if sum&(1<<b) != 0 {
					votes[b]++
				} else {
					votes[b]--
				}
			}
		}
	}
	var fp uint64
	for b, v := range votes {
		if v > 0 {
			fp |= 1 << b
		}
	}
	return fp
}

// Hamming is the number of differing bits: one XOR plus one POPCNT.
func Hamming(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// Nearest scans fingerprints linearly and returns the index and Hamming
// distance of the closest one, or (-1, 65) for an empty index. A linear
// scan is deliberate: at personal scale (<= 10^4 atoms, 80 KB) it beats an
// LSH index, whose 4x16-bit bands would miss most near duplicates
// (ADR-020 adversarial review, finding 1).
func Nearest(fingerprints []uint64, q uint64) (index, distance int) {
	index, distance = -1, 65
	for i, fp := range fingerprints {
		if d := Hamming(fp, q); d < distance {
			index, distance = i, d
		}
	}
	return index, distance
}

func normalizeForFingerprint(text string) string {
	var b strings.Builder
	space := false
	// NFC first: "ã" precomposed (Windows/Linux) and decomposed (pasted
	// from macOS) must produce the same n-grams.
	for _, r := range strings.TrimSpace(norm.NFC.String(text)) {
		// Punctuation is treated as a word break: a repeat that differs
		// only in commas or a final period is the same text, while a
		// changed letter or digit (a different value) still counts.
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

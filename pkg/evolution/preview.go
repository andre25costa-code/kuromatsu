package evolution

import (
	"strconv"
	"strings"
)

type DraftPreview struct {
	CurrentBody  string
	RenderedBody string
	DiffPreview  string
}

func buildLineDiffPreview(currentBody, renderedBody string) string {
	before := strings.Split(strings.TrimRight(currentBody, "\n"), "\n")
	after := strings.Split(strings.TrimRight(renderedBody, "\n"), "\n")

	if len(before) == 1 && before[0] == "" {
		before = nil
	}
	if len(after) == 1 && after[0] == "" {
		after = nil
	}

	prefixLen := sharedPrefixLen(before, after)
	suffixLen := sharedSuffixLen(before[prefixLen:], after[prefixLen:])
	const contextRadius = 2

	beforeChangeStart := prefixLen
	beforeChangeEnd := len(before) - suffixLen
	afterChangeStart := prefixLen
	afterChangeEnd := len(after) - suffixLen

	hunkBeforeStart := previewMaxInt(0, beforeChangeStart-contextRadius)
	hunkAfterStart := previewMaxInt(0, afterChangeStart-contextRadius)
	hunkBeforeEnd := previewMinInt(len(before), beforeChangeEnd+contextRadius)
	hunkAfterEnd := previewMinInt(len(after), afterChangeEnd+contextRadius)

	removed := before[prefixLen : len(before)-suffixLen]
	added := after[prefixLen : len(after)-suffixLen]
	if len(removed) == 0 && len(added) == 0 {
		return "(no content change)"
	}

	lines := make([]string, 0, (hunkBeforeEnd-hunkBeforeStart)+(hunkAfterEnd-hunkAfterStart))
	header := make([]string, 0, 3+len(lines))
	header = append(header,
		"--- current",
		"+++ rendered",
		formatUnifiedHunkHeader(
			hunkBeforeStart,
			hunkBeforeEnd-hunkBeforeStart,
			hunkAfterStart,
			hunkAfterEnd-hunkAfterStart,
		),
	)
	for _, line := range before[hunkBeforeStart:beforeChangeStart] {
		lines = append(lines, " "+line)
	}
	for _, line := range removed {
		lines = append(lines, "-"+line)
	}
	for _, line := range added {
		lines = append(lines, "+"+line)
	}
	for _, line := range after[afterChangeEnd:hunkAfterEnd] {
		lines = append(lines, " "+line)
	}
	return strings.Join(append(header, lines...), "\n")
}

func formatUnifiedHunkHeader(beforeStart, beforeCount, afterStart, afterCount int) string {
	return "@@ -" + formatUnifiedRange(
		beforeStart+1,
		beforeCount,
	) + " +" + formatUnifiedRange(
		afterStart+1,
		afterCount,
	) + " @@"
}

func formatUnifiedRange(start, count int) string {
	return strconv.Itoa(start) + "," + strconv.Itoa(count)
}

func sharedPrefixLen(left, right []string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	n := 0
	for n < limit && left[n] == right[n] {
		n++
	}
	return n
}

func sharedSuffixLen(left, right []string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	n := 0
	for n < limit && left[len(left)-1-n] == right[len(right)-1-n] {
		n++
	}
	return n
}

func previewMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func previewMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

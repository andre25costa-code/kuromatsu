package kuromatsu

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

var (
	// A Markdown link target or an HTML src/href attribute.
	readmeLinkRe    = regexp.MustCompile(`\]\(([^)\s]+)\)|(?:src|href)="([^"]+)"`)
	readmeHeadingRe = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)
)

// readmeSlug is GitHub's anchor for a plain heading: lower case, spaces
// become "-", punctuation is dropped, letters (accented ones too), digits,
// "-" and "_" stay. Headings that need more than this should be renamed.
func readmeSlug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Every relative link and image in README.md points to a file in the
// repository, and every in-page link points to a heading. External links
// are not checked here: that would need the network.
func TestREADME_RelativeLinksResolve(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	// Fenced code is neither links nor headings ("# comment" in a bash block).
	var prose []string
	anchors := map[string]bool{}
	inFence := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		prose = append(prose, line)
		if m := readmeHeadingRe.FindStringSubmatch(line); m != nil {
			anchors[readmeSlug(m[1])] = true
		}
	}

	checked := 0
	for _, m := range readmeLinkRe.FindAllStringSubmatch(strings.Join(prose, "\n"), -1) {
		target := m[1] + m[2]
		switch {
		case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"),
			strings.HasPrefix(target, "mailto:"):
			continue
		case strings.HasPrefix(target, "#"):
			if !anchors[target[1:]] {
				t.Errorf("README.md links to missing heading %q", target)
			}
			checked++
			continue
		}
		if i := strings.IndexByte(target, '#'); i >= 0 {
			target = target[:i]
		}
		if _, err := os.Stat(target); err != nil {
			t.Errorf("README.md links to missing %q", target)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no relative links found in README.md; the link pattern is broken")
	}
}

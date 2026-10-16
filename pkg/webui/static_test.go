package webui

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticAssets_SizeBudget(t *testing.T) {
	js, err := fs.ReadFile(staticFiles, "static/app.js")
	require.NoError(t, err)
	css, err := fs.ReadFile(staticFiles, "static/app.css")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(js), 15*1024, "app.js budget")
	assert.LessOrEqual(t, len(css), 6*1024, "app.css budget")
}

// The CSP forbids inline script and style, so the page must not use them;
// and server data must never be inserted as HTML.
func TestStaticAssets_CSPCompatible(t *testing.T) {
	html, err := fs.ReadFile(staticFiles, "static/index.html")
	require.NoError(t, err)
	page := string(html)
	assert.NotRegexp(t, regexp.MustCompile(`<script(?:\s[^>]*)?>[^<]+</script>`), page, "inline script")
	assert.NotRegexp(t, regexp.MustCompile(`\son[a-z]+=`), page, "inline event handler")
	assert.NotContains(t, page, "style=")
	assert.NotContains(t, page, "<style")
	assert.Equal(t, 7, strings.Count(page, "<section "), "seven screens")

	js, err := fs.ReadFile(staticFiles, "static/app.js")
	require.NoError(t, err)
	for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval("} {
		assert.NotContains(t, string(js), bad)
	}
}

// Every form control has a label pointing at it.
func TestStaticAssets_LabelsForInputs(t *testing.T) {
	html, err := fs.ReadFile(staticFiles, "static/index.html")
	require.NoError(t, err)
	page := string(html)
	ids := regexp.MustCompile(`<(?:input|select|textarea)[^>]*\sid="([^"]+)"`).FindAllStringSubmatch(page, -1)
	require.NotEmpty(t, ids)
	for _, m := range ids {
		assert.Contains(t, page, `for="`+m[1]+`"`, "control #%s has no label", m[1])
	}
}

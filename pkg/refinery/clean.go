package refinery

import (
	"regexp"
	"strings"
)

// Ported from context-refinery's clean.py (FR-024). Deliberately
// conservative: noise is removed, words are not.
var (
	ansiPattern        = regexp.MustCompile(`\x1b(?:[@-Z\\\-_]|\[[0-?]*[ -/]*[@-~])`)
	controlPattern     = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
	htmlCommentPattern = regexp.MustCompile(`(?s)<!--.*?-->`)
	fencedCodePattern  = regexp.MustCompile("(?s)```.*?```")
	assistantAck       = regexp.MustCompile(
		`(?i)^\s*(?:ok(?:ay)?|certo|entendido|perfeito|combinado|claro|beleza|feito)[.!…\s]*$`,
	)
)

// SecretPattern is one credential shape; Group selects the submatch holding
// the secret itself, so a "Bearer " or "token=" prefix stays readable
// (0 = the whole match).
type SecretPattern struct {
	Re    *regexp.Regexp
	Group int
}

// SecretPatterns are the credential shapes from clean.py, shared by the
// provider-side masking (FR-029) and the atoms' "sensitive" flag (AC-024-3).
var SecretPatterns = []SecretPattern{
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}`), 0},
	{regexp.MustCompile(`\b(?:ghp|github_pat)_[A-Za-z0-9_]{16,}`), 0},
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), 0},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), 0},
	{regexp.MustCompile(`(?i)\bBearer\s+([A-Za-z0-9._~+/-]{16,})`), 1},
	{
		regexp.MustCompile(
			`(?i)\b(?:api[_-]?key|access[_-]?token|token|password|passwd|secret)\s*[:=]\s*["']?([^\s"']{6,})`,
		),
		1,
	},
}

// Clean removes ANSI sequences, control characters and HTML comments from
// text headed for memory or a sleep digest (AC-024-1). Fenced code blocks
// are kept byte for byte.
func Clean(text string) string {
	var b strings.Builder
	last := 0
	for _, loc := range fencedCodePattern.FindAllStringIndex(text, -1) {
		b.WriteString(cleanProse(text[last:loc[0]]))
		b.WriteString(text[loc[0]:loc[1]])
		last = loc[1]
	}
	b.WriteString(cleanProse(text[last:]))
	return b.String()
}

func cleanProse(s string) string {
	return htmlCommentPattern.ReplaceAllString(CleanUserText(s), "")
}

// CleanUserText strips only terminal noise (ANSI, control characters):
// a user's words are never removed (AC-024-5).
func CleanUserText(text string) string {
	return controlPattern.ReplaceAllString(ansiPattern.ReplaceAllString(text, ""), "")
}

// IsAssistantAck reports an acknowledgement-only assistant turn ("ok",
// "entendido", ...) that carries nothing worth consolidating (AC-024-2).
func IsAssistantAck(text string) bool {
	return strings.TrimSpace(text) != "" && assistantAck.MatchString(text)
}

// ContainsCredential reports whether text holds a credential -- used to
// flag an atom "sensitive", never to remove the value (AC-024-3, P4).
func ContainsCredential(text string) bool {
	for _, p := range SecretPatterns {
		if p.Re.MatchString(text) {
			return true
		}
	}
	return false
}

// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/providers"
)

// maxUnknownToolHintDistance caps how far (Levenshtein) a candidate name
// can be from the requested one before it's not worth suggesting — beyond
// this it's more likely to confuse the model than help it self-correct.
const maxUnknownToolHintDistance = 3

// unknownToolResultMessage builds the tool-result message for a name that
// doesn't exist anywhere in the agent's global tool registry (ADR-014
// point 5.1 / FR-014 AC-014-5): a similar-name suggestion plus the tools
// available in the active focus window. Only used when focus is actually
// active for this turn — see ExecuteTools' call site.
func unknownToolResultMessage(ts *turnState, name, toolCallID string) providers.Message {
	candidates := ts.profile.AllowedTools
	if ts.profile.ToolsMode != config.TurnProfileModeCustom {
		// The window imposes no custom allow-list (e.g. "full") — every
		// globally registered tool is "available" here.
		candidates = ts.agent.Tools.List()
	}
	return providers.Message{
		Role:       "tool",
		Content:    unknownToolHint(name, candidates),
		ToolCallID: toolCallID,
	}
}

// unknownToolHint builds the hint text itself: a similar-name suggestion
// (if any candidate is close enough) plus the tools available in the
// active window, so the model can self-correct on its very next iteration
// paying only the delta (ADR-015 prefix cache), not a full re-prefill.
func unknownToolHint(name string, candidates []string) string {
	sorted := append([]string(nil), candidates...)
	sort.Strings(sorted)
	available := strings.Join(sorted, ", ")

	best, distance := closestToolName(name, candidates)
	switch {
	case best != "" && distance <= maxUnknownToolHintDistance:
		return fmt.Sprintf(
			"ferramenta %q não existe; parecida: %s — disponíveis nesta janela: %s",
			name, best, available,
		)
	case available != "":
		return fmt.Sprintf("ferramenta %q não existe; disponíveis nesta janela: %s", name, available)
	default:
		return fmt.Sprintf("ferramenta %q não existe; nenhuma ferramenta disponível nesta janela", name)
	}
}

// closestToolName returns the candidate with the smallest Levenshtein
// distance to target (case-insensitive), and that distance. distance is -1
// when candidates is empty.
func closestToolName(target string, candidates []string) (best string, distance int) {
	distance = -1
	target = strings.ToLower(target)
	for _, candidate := range candidates {
		d := levenshteinDistance(target, strings.ToLower(candidate))
		if distance == -1 || d < distance {
			distance = d
			best = candidate
		}
	}
	return best, distance
}

// levenshteinDistance computes the classic edit distance between a and b
// (insertions/deletions/substitutions, unit cost), operating on runes so
// PT-BR accented tool/argument names measure correctly. O(len(a)*len(b))
// time, O(min(len(a),len(b))) space.
func levenshteinDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	if len(ar) < len(br) {
		ar, br = br, ar
	}

	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	curr := make([]int, len(br)+1)

	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			deletion := prev[j] + 1
			insertion := curr[j-1] + 1
			substitution := prev[j-1] + cost
			m := deletion
			if insertion < m {
				m = insertion
			}
			if substitution < m {
				m = substitution
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

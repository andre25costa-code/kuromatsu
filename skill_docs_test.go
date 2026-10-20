package kuromatsu

import (
	"bufio"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
)

const docsSkillDir = "workspace/skills/kuromatsu-docs"

// The pages the agent and the README point to; each must ship in the binary.
var docsSkillPages = []string{
	"SKILL.md",
	"references/config.md",
	"references/scheduling.md",
	"references/tools.md",
	"references/channels.md",
	"references/deploy.md",
	"references/testing.md",
	"references/webui.md",
	"references/install.md",
	"references/automations.md",
}

func TestKuromatsuDocsSkill_EmbeddedPages(t *testing.T) {
	for _, page := range docsSkillPages {
		p := path.Join(docsSkillDir, page)
		if _, err := fs.Stat(OnboardWorkspace, p); err != nil {
			t.Errorf("kuromatsu-docs: %s is not embedded", p)
		}
	}
}

var (
	mdLinkRe   = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	codeSpanRe = regexp.MustCompile("`([^`]+)`")
	// A repository path: relative, at least one directory, optional file.
	repoPathRe = regexp.MustCompile(`^(?:\.{1,2}/)?(?:[\w.-]+/)+[\w.-]*$`)
	// A ":symbol", ":line" or ":from-to" suffix after the file name.
	locationSuffixRe = regexp.MustCompile(`:[^/]*$`)
)

// repoRoots are the first path segments that name a directory of this
// repository. Paths under other roots (memory/MEMORY.md, cron/jobs.json)
// are relative to the user's workspace or data directory and not checked.
var repoRoots = map[string]bool{
	"pkg": true, "cmd": true, "deploy": true, "scripts": true, "docker": true,
	"config": true, "workspace": true, ".github": true, "assets": true,
}

// repoPathSkip marks spans that are placeholders or outside the repository.
var repoPathSkip = []string{"~/", "$", "<", "*", "github.com/"}

func TestKuromatsuDocsSkill_LinksResolve(t *testing.T) {
	linkedFromSkill := map[string]bool{}
	var references []string

	err := fs.WalkDir(OnboardWorkspace, docsSkillDir, func(file string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(file, ".md") {
			return err
		}
		if path.Dir(file) == docsSkillDir+"/references" {
			references = append(references, file)
		}
		data, err := fs.ReadFile(OnboardWorkspace, file)
		if err != nil {
			return err
		}
		inFence := false
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for line := 1; sc.Scan(); line++ {
			text := sc.Text()
			if strings.HasPrefix(strings.TrimSpace(text), "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			for _, m := range mdLinkRe.FindAllStringSubmatch(text, -1) {
				target := m[1]
				if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
					strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") {
					continue
				}
				target, _, _ = strings.Cut(target, "#")
				resolved := path.Join(path.Dir(file), target)
				if _, err := fs.Stat(OnboardWorkspace, resolved); err != nil {
					t.Errorf("%s:%d: link to missing %q", file, line, m[1])
					continue
				}
				if file == docsSkillDir+"/SKILL.md" {
					linkedFromSkill[resolved] = true
				}
			}
			for _, m := range codeSpanRe.FindAllStringSubmatch(text, -1) {
				checkRepoPath(t, file, line, m[1])
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(references) == 0 {
		t.Fatalf("no reference pages found under %s/references", docsSkillDir)
	}
	for _, ref := range references {
		if !linkedFromSkill[ref] {
			t.Errorf("%s is not linked from SKILL.md", ref)
		}
	}
}

// checkRepoPath reports a code span that names a repository path which does
// not exist in this checkout.
func checkRepoPath(t *testing.T, file string, line int, span string) {
	t.Helper()
	if strings.Contains(span, "{") {
		return
	}
	for _, s := range repoPathSkip {
		if strings.Contains(span, s) {
			return
		}
	}
	p := locationSuffixRe.ReplaceAllString(span, "")
	if !repoPathRe.MatchString(p) {
		return
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(p, "./"), "/")
	if !repoRoots[first] {
		return
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("%s:%d: reference to missing repo path %q", file, line, span)
	}
}

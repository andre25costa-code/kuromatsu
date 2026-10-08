package skills

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// builtinSkillsRoot is where the builtin skills live inside the embedded
// onboarding workspace (kuromatsu.OnboardWorkspace).
const builtinSkillsRoot = "workspace/skills"

type builtinSkill struct {
	Name        string
	Description string
}

// builtinSkills lists the skills embedded in the binary, with the
// description from each SKILL.md frontmatter.
func builtinSkills(fsys fs.FS) ([]builtinSkill, error) {
	entries, err := fs.ReadDir(fsys, builtinSkillsRoot)
	if err != nil {
		return nil, fmt.Errorf("reading embedded skills: %w", err)
	}
	var list []builtinSkill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		list = append(list, builtinSkill{
			Name:        e.Name(),
			Description: skillDescription(fsys, path.Join(builtinSkillsRoot, e.Name(), "SKILL.md")),
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// skillDescription returns the `description:` field of a SKILL.md
// frontmatter, or "" when there is none.
func skillDescription(fsys fs.FS, name string) string {
	f, err := fsys.Open(name)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return ""
	}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		if v, ok := strings.CutPrefix(line, "description:"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// installBuiltinSkills copies each embedded skill that is missing from
// <workspace>/skills. A skill directory that already exists is the user's and
// is left as is (kept), like onboard does with the workspace files.
func installBuiltinSkills(fsys fs.FS, workspace string) (installed, kept []string, err error) {
	list, err := builtinSkills(fsys)
	if err != nil {
		return nil, nil, err
	}
	for _, s := range list {
		target := filepath.Join(workspace, "skills", s.Name)
		if _, statErr := os.Stat(target); statErr == nil {
			kept = append(kept, s.Name)
			continue
		}
		if err := copyEmbeddedDir(fsys, path.Join(builtinSkillsRoot, s.Name), target); err != nil {
			return installed, kept, fmt.Errorf("installing %s: %w", s.Name, err)
		}
		installed = append(installed, s.Name)
	}
	return installed, kept, nil
}

func copyEmbeddedDir(fsys fs.FS, root, target string) error {
	return fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(p, ".sh") {
			mode = 0o755
		}
		return os.WriteFile(dst, data, mode)
	})
}

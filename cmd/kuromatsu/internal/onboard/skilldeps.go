package onboard

import (
	"io/fs"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Seams for tests: where a tool is looked up, and which OS this is.
var (
	lookPath = exec.LookPath
	goos     = runtime.GOOS
)

// skillMissing returns what this machine lacks for an embedded skill, read
// from its SKILL.md frontmatter (metadata.nanobot.requires.bins and
// metadata.nanobot.os): each missing tool, and "os: <goos>" when the skill
// lists the OSes it supports and this one is not among them. A skill without
// that metadata needs nothing.
func skillMissing(fsys fs.FS, skillDir string) []string {
	data, err := fs.ReadFile(fsys, path.Join(skillDir, "SKILL.md"))
	if err != nil {
		return nil
	}
	front, ok := frontmatterOf(string(data))
	if !ok {
		return nil
	}
	var meta struct {
		Metadata struct {
			Nanobot struct {
				OS       []string `yaml:"os"`
				Requires struct {
					Bins []string `yaml:"bins"`
				} `yaml:"requires"`
			} `yaml:"nanobot"`
		} `yaml:"metadata"`
	}
	if yaml.Unmarshal([]byte(front), &meta) != nil {
		return nil
	}

	var missing []string
	if oses := meta.Metadata.Nanobot.OS; len(oses) > 0 && !slices.Contains(oses, goos) {
		missing = append(missing, "os: "+goos)
	}
	for _, bin := range meta.Metadata.Nanobot.Requires.Bins {
		if _, err := lookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	return missing
}

func frontmatterOf(content string) (string, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return "", false
	}
	front, _, ok := strings.Cut(rest, "\n---")
	return front, ok
}

package config

import (
	"fmt"
	"runtime"
	"strings"
)

func windowsAbsolutePath(value string) bool {
	return strings.HasPrefix(value, `\\`) ||
		(len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/'))
}

// ValidatePlatformPaths rejects foreign absolute paths on non-Windows hosts.
func (c *Config) ValidatePlatformPaths() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	paths := map[string]string{"agents.defaults.workspace": c.Agents.Defaults.Workspace}
	for i, agent := range c.Agents.List {
		paths[fmt.Sprintf("agents.list[%d].workspace", i)] = agent.Workspace
	}
	for i, model := range c.ModelList {
		if model != nil {
			paths[fmt.Sprintf("model_list[%d].workspace", i)] = model.Workspace
		}
	}
	for key, path := range paths {
		if windowsAbsolutePath(path) {
			return fmt.Errorf("%s: Windows absolute path is invalid on %s; configure a native path", key, runtime.GOOS)
		}
	}
	return nil
}

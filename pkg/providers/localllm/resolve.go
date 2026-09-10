package localllm

import (
	"os"
	"path/filepath"
	"strings"
)

// EnvModelsDir names the directory the runtime searches for GGUF files
// before falling back to <home>/models and ./models. This is a
// Kuromatsu-native variable (no PicoClaw predecessor), so it does not need
// the PICOCLAW_*/KUROMATSU_* compatibility shim used for renamed variables
// (ADR-005).
const EnvModelsDir = "KUROMATSU_MODELS_DIR"

// ResolveModelPath finds the GGUF file for modelID. home is the caller's
// resolved application home directory (e.g. config.GetHome(), which this
// package does not import to avoid pulling pkg/config into every build of
// localllm). Search order: an absolute/relative path used as-is,
// $KUROMATSU_MODELS_DIR/<id>.gguf, <home>/models/<id>.gguf, ./models/<id>.gguf.
func ResolveModelPath(modelID, home string) (string, error) {
	if modelID == "" {
		return "", ErrModelNotFound
	}

	if filepath.IsAbs(modelID) || strings.ContainsAny(modelID, `/\`) {
		if fileExists(modelID) {
			return modelID, nil
		}
	}

	filename := modelID
	if filepath.Ext(filename) != ".gguf" {
		filename += ".gguf"
	}

	candidates := make([]string, 0, 3)
	if dir := os.Getenv(EnvModelsDir); dir != "" {
		candidates = append(candidates, filepath.Join(dir, filename))
	}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, "models", filename))
	}
	candidates = append(candidates, filepath.Join("models", filename))

	for _, c := range candidates {
		if fileExists(c) {
			return c, nil
		}
	}
	return "", ErrModelNotFound
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

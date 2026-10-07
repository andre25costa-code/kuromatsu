package providers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

// fakeModelPath returns an absolute path (containing a separator, so
// ResolveModelPath treats it as a literal path rather than a models-dir
// lookup) to a real, empty file -- nativeOptionsFromModelConfig only needs
// the path to exist, never opens it.
func fakeModelPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake.gguf")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestNativeOptionsFromModelConfig_CoreCacheParking is B2/ADR-015 point 8:
// core_cache_parking in ExtraBody must map to Options.CoreCacheParking,
// defaulting to false (off, byte-identical to pre-B2 behavior) when unset.
func TestNativeOptionsFromModelConfig_CoreCacheParking(t *testing.T) {
	path := fakeModelPath(t)

	cases := []struct {
		name      string
		extraBody map[string]any
		want      bool
	}{
		{"unset defaults to off", nil, false},
		{"explicit false", map[string]any{"core_cache_parking": false}, false},
		{"explicit true", map[string]any{"core_cache_parking": true}, true},
		{"wrong type ignored, stays off", map[string]any{"core_cache_parking": "true"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := nativeOptionsFromModelConfig(&config.ModelConfig{ExtraBody: tc.extraBody}, path)
			if err != nil {
				t.Fatalf("nativeOptionsFromModelConfig() error = %v", err)
			}
			if opts.CoreCacheParking != tc.want {
				t.Fatalf("CoreCacheParking = %v, want %v", opts.CoreCacheParking, tc.want)
			}
		})
	}
}

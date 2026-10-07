package common

import "testing"

func assertSchemaKeyAbsent(t *testing.T, value any, key string) {
	t.Helper()

	switch typed := value.(type) {
	case map[string]any:
		if _, found := typed[key]; found {
			t.Fatalf("schema still contains key %q: %#v", key, typed)
		}
		for _, nested := range typed {
			assertSchemaKeyAbsent(t, nested, key)
		}
	case []any:
		for _, nested := range typed {
			assertSchemaKeyAbsent(t, nested, key)
		}
	case []string:
		return
	}
}

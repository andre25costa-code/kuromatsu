package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandHome(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tt := range tests {
		result := ExpandHome(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestExpandHomeWithTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	result := ExpandHome("~/path")
	assert.Equal(t, home+"/path", result)

	result = ExpandHome("~")
	assert.Equal(t, home, result)
}

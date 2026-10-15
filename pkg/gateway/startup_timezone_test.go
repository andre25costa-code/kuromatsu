package gateway

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
)

func TestLoadStartupConfig_AppliesTimezone(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	t.Setenv("TZ", "")
	orig := time.Local
	t.Cleanup(func() {
		time.Local = orig //nolint:gosmopolitan // restore the process timezone after the test
		config.ResetProcessTimezoneForTest()
	})
	config.ResetProcessTimezoneForTest()

	path := filepath.Join(home, "config.json")
	cfg := config.DefaultConfig()
	cfg.Timezone = "Asia/Tokyo"
	require.NoError(t, config.SaveConfig(path, cfg))

	_, err := loadStartupConfig(path)
	require.NoError(t, err)
	require.Equal(t, "Asia/Tokyo", time.Local.String())
}

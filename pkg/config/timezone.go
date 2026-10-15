package config

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/logger"
)

// ValidateTimezone checks that a non-empty timezone names a zone
// time.LoadLocation knows. It never changes time.Local.
func (c *Config) ValidateTimezone() error {
	if c.Timezone == "" {
		return nil
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("timezone %q: %w", c.Timezone, err)
	}
	return nil
}

// processTimezoneOnce makes ApplyProcessTimezone act at most once per
// process; tests reset it through ResetProcessTimezoneForTest.
var processTimezoneOnce sync.Once

// ApplyProcessTimezone sets time.Local from cfg.Timezone, once per process,
// unless the TZ environment variable is set (TZ wins). It must run at
// startup, before goroutines read the clock: writing time.Local later would
// race with them, which is why config reloads never call it. Changing the
// timezone therefore needs a restart.
func ApplyProcessTimezone(cfg *Config) {
	processTimezoneOnce.Do(func() {
		if cfg == nil || cfg.Timezone == "" || os.Getenv("TZ") != "" {
			return
		}
		loc, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			logger.WarnCF("config", "timezone not applied",
				map[string]any{"timezone": cfg.Timezone, "error": err.Error()})
			return
		}
		time.Local = loc //nolint:gosmopolitan // the configured timezone is the process timezone
	})
}

// ResetProcessTimezoneForTest lets a test run ApplyProcessTimezone again.
// Callers restore time.Local themselves.
func ResetProcessTimezoneForTest() {
	processTimezoneOnce = sync.Once{}
}

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig_DuckDuckGoEnabledAlongsideSogou(t *testing.T) {
	web := DefaultConfig().Tools.Web
	if !web.DuckDuckGo.Enabled {
		t.Fatal("DefaultConfig().Tools.Web.DuckDuckGo.Enabled should be true")
	}
	if !web.Sogou.Enabled {
		t.Fatal("DefaultConfig().Tools.Web.Sogou.Enabled should stay true")
	}
}

func TestConfigExample_DuckDuckGoEnabled(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "config.example.json"))
	if err != nil {
		t.Fatalf("ReadFile(config.example.json) error: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Unmarshal(config.example.json) error: %v", err)
	}
	if !cfg.Tools.Web.DuckDuckGo.Enabled {
		t.Fatal("config.example.json tools.web.duckduckgo.enabled should be true")
	}
	if cfg.Tools.Web.DuckDuckGo.MaxResults != 5 {
		t.Fatalf("config.example.json tools.web.duckduckgo.max_results = %d, want 5",
			cfg.Tools.Web.DuckDuckGo.MaxResults)
	}
}

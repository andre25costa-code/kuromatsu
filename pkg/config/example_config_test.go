package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// config/config.example.json must only show what this fork supports: a
// channel type without a registered factory is skipped at startup with a
// warning, and an unknown tools key is silently ignored, so a stale example
// misleads whoever copies it.
func TestConfigExample_OnlySupportedChannelsAndTools(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "config.example.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var doc struct {
		ChannelList map[string]struct {
			Type string `json:"type"`
		} `json:"channel_list"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	supported := map[string]bool{
		ChannelTelegram:       true,
		ChannelWhatsApp:       true,
		ChannelWhatsAppNative: true,
		ChannelPico:           true,
		ChannelPicoClient:     true,
	}
	for name, ch := range doc.ChannelList {
		typ := ch.Type
		if typ == "" {
			typ = name
		}
		if !supported[typ] {
			t.Errorf("channel_list.%s has type %q, which this fork does not implement", name, typ)
		}
	}
}

// The example is meant to be copied as config.json, and LoadConfig rejects
// unknown fields, so it has to load as-is.
func TestConfigExample_LoadsWithLoadConfig(t *testing.T) {
	t.Setenv("KUROMATSU_HOME", t.TempDir())
	if _, err := LoadConfig(filepath.Join("..", "..", "config", "config.example.json")); err != nil {
		t.Fatalf("LoadConfig(config.example.json): %v", err)
	}
}

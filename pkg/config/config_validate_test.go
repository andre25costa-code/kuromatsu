package config

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// writeConfigWithFileSecret writes a config whose only model reads its API
// key from the relative reference file://secret.txt, and the secret file
// itself, into dir. It returns the config path.
func writeConfigWithFileSecret(t *testing.T, dir string) string {
	t.Helper()
	updateResolver(dir)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("real-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := `{"version":` + strconv.Itoa(CurrentVersion) +
		`,"model_list":[{"model_name":"test","model":"openai/gpt-4"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveSecurityConfig(filepath.Join(dir, SecurityConfigFile), &Config{ModelList: SecureModelList{
		&ModelConfig{ModelName: "test", APIKeys: SimpleSecureStrings("file://secret.txt")},
	}}); err != nil {
		t.Fatal(err)
	}
	return path
}

func isolateValidateEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("KUROMATSU_HOME", t.TempDir())
	t.Setenv("KUROMATSU_KEY_PASSPHRASE", "")
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	return scratch
}

// The scratch copy is loaded with the resolver already in place, so a
// relative file:// secret that exists only next to the real config resolves,
// and a concurrent resolution through the process-wide resolver keeps
// reading the real directory while validations run.
func TestValidateForSave_NoGlobalResolverChange(t *testing.T) {
	isolateValidateEnv(t)
	realDir := t.TempDir()
	path := writeConfigWithFileSecret(t, realDir)

	cfg, err := LoadConfigForEdit(path)
	if err != nil {
		t.Fatalf("LoadConfigForEdit: %v", err)
	}
	if got := cfg.ModelList[0].APIKey(); got != "real-secret" {
		t.Fatalf("api key = %q, want the real secret", got)
	}
	localBefore := time.Local

	var stop atomic.Bool
	var wrong atomic.Value
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			v, rerr := resolveKey("file://secret.txt")
			if rerr != nil || v != "real-secret" {
				wrong.Store(v + " / " + errString(rerr))
				return
			}
		}
	}()
	for i := 0; i < 20; i++ {
		if err := ValidateForSave(cfg); err != nil {
			stop.Store(true)
			wg.Wait()
			t.Fatalf("ValidateForSave: %v", err)
		}
	}
	stop.Store(true)
	wg.Wait()
	if w := wrong.Load(); w != nil {
		t.Fatalf("concurrent resolution saw %q during validation", w)
	}
	if v, err := resolveKey("file://secret.txt"); err != nil || v != "real-secret" {
		t.Fatalf("resolver after validation = %q, %v; want the real directory", v, err)
	}
	if time.Local != localBefore {
		t.Fatal("ValidateForSave changed time.Local")
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// An invalid config is refused and leaves nothing behind: the scratch
// directory is removed and the real config directory is untouched.
func TestValidateForSave_RejectsInvalid(t *testing.T) {
	scratch := isolateValidateEnv(t)
	realDir := t.TempDir()
	path := writeConfigWithFileSecret(t, realDir)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigForEdit(path)
	if err != nil {
		t.Fatalf("LoadConfigForEdit: %v", err)
	}
	cfg.Timezone = "Mars/Olympus_Mons"
	if err = ValidateForSave(cfg); err == nil {
		t.Fatal("ValidateForSave accepted an unknown timezone")
	}

	cfg2 := DefaultConfig()
	cfg2.Channels = ChannelsConfig{"x": &Channel{Type: "no-such-channel-type"}}
	if err = ValidateForSave(cfg2); err == nil {
		t.Fatal("ValidateForSave accepted an unknown channel type")
	}

	if entries, _ := os.ReadDir(scratch); len(entries) != 0 {
		t.Fatalf("scratch files left behind: %v", entries)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("ValidateForSave changed the real config file")
	}
	entries, _ := os.ReadDir(realDir)
	if len(entries) != 3 { // config.json, the security file, secret.txt
		t.Fatalf("real config dir has %d entries, want 3", len(entries))
	}
}

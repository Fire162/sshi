package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigSaveAndResolve(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sshi-config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfgPath := filepath.Join(tmpDir, "config.json")
	cfg, err := LoadConfigFromPath(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfigFromPath failed: %v", err)
	}

	rawTarget := "https://sshx.io/s/hr2hQ8YKuf#lFRwv8c7cZhJRd"

	// 1. Save session by alias
	_, err = cfg.SaveSession("prod", rawTarget, "production cluster")
	if err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	// 2. Resolve alias
	resolved, err := cfg.ResolveTarget("prod")
	if err != nil {
		t.Fatalf("ResolveTarget failed: %v", err)
	}
	if resolved != rawTarget {
		t.Fatalf("resolved target mismatch: got %q, want %q", resolved, rawTarget)
	}

	// 3. Resolve empty string (should return last session)
	resolvedLast, err := cfg.ResolveTarget("")
	if err != nil {
		t.Fatalf("ResolveTarget empty failed: %v", err)
	}
	if resolvedLast != rawTarget {
		t.Fatalf("expected last session to match %q, got %q", rawTarget, resolvedLast)
	}

	// 4. Reload from disk to verify persistence and permissions
	cfgReloaded, err := LoadConfigFromPath(cfgPath)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if len(cfgReloaded.ListSessions()) != 1 {
		t.Fatalf("expected 1 session, got %d", len(cfgReloaded.ListSessions()))
	}

	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 file permission, got %o", info.Mode().Perm())
	}

	// 5. Test invalid alias rejection
	_, err = cfgReloaded.SaveSession("../../malicious", rawTarget, "")
	if err == nil {
		t.Errorf("expected error for path traversal alias, got nil")
	}

	// 6. Delete session
	if err := cfgReloaded.DeleteSession("prod"); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	if len(cfgReloaded.ListSessions()) != 0 {
		t.Fatalf("expected 0 sessions after delete, got %d", len(cfgReloaded.ListSessions()))
	}
}

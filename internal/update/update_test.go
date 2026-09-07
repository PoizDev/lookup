package update

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompareSemVerAndChannels(t *testing.T) {
	tests := []struct {
		current, latest string
		want            int
	}{
		{"v0.1.0", "v0.1.1", -1}, {"v0.2.0", "v0.1.9", 1}, {"v1.0.0", "v1.0.0", 0},
		{"v0.2.0-rc.1", "v0.2.0", -1}, {"v0.2.0-rc.2", "v0.2.0-rc.10", -1},
	}
	for _, tt := range tests {
		got, err := Compare(tt.current, tt.latest)
		if err != nil || got != tt.want {
			t.Errorf("Compare(%q,%q)=(%d,%v), want %d", tt.current, tt.latest, got, err, tt.want)
		}
	}
	if IsReleaseVersion("dev") || IsReleaseVersion("unknown") || !IsReleaseVersion("0.1.0") {
		t.Fatal("release version classification is incorrect")
	}
}

func TestCacheFreshnessAndNotificationCooldown(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "update.json")
	store := CacheStore{Path: path, Now: func() time.Time { return now }}
	if _, fresh := store.LoadFresh(24 * time.Hour); fresh {
		t.Fatal("missing cache reported fresh")
	}
	state := Cache{CheckedAt: now.Add(-time.Hour), LatestVersion: "v0.1.1"}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if got, fresh := store.LoadFresh(24 * time.Hour); !fresh || got.LatestVersion != "v0.1.1" {
		t.Fatalf("fresh cache=(%+v,%v)", got, fresh)
	}
	now = now.Add(25 * time.Hour)
	if _, fresh := store.LoadFresh(24 * time.Hour); fresh {
		t.Fatal("expired cache reported fresh")
	}
	now = now.Add(-25 * time.Hour)
	if !ShouldNotify(state, "v0.1.1", now, 7*24*time.Hour) {
		t.Fatal("first notification suppressed")
	}
	state.LastNotifiedVersion, state.LastNotifiedAt = "v0.1.1", now
	if ShouldNotify(state, "v0.1.1", now.Add(6*24*time.Hour), 7*24*time.Hour) {
		t.Fatal("cooldown ignored")
	}
	if !ShouldNotify(state, "v0.1.2", now.Add(time.Hour), 7*24*time.Hour) {
		t.Fatal("new version was not immediately notified")
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, fresh := store.LoadFresh(24 * time.Hour); fresh {
		t.Fatal("corrupt cache reported fresh")
	}
}

func TestArtifactsAndChecksums(t *testing.T) {
	cases := map[string]string{
		"linux/amd64": "lookup_0.1.1_linux_amd64_musl.tar.gz", "linux/arm64": "lookup_0.1.1_linux_arm64_musl.tar.gz",
		"darwin/amd64": "lookup_0.1.1_darwin_amd64.tar.gz", "darwin/arm64": "lookup_0.1.1_darwin_arm64.tar.gz",
		"windows/amd64": "lookup_0.1.1_windows_amd64.zip",
	}
	for platform, want := range cases {
		parts := splitPlatform(platform)
		got, err := ArtifactName("v0.1.1", parts[0], parts[1])
		if err != nil || got != want {
			t.Errorf("ArtifactName(%s)=(%q,%v), want %q", platform, got, err, want)
		}
	}
	payload := []byte("verified archive")
	sum := sha256.Sum256(payload)
	checksums := hex.EncodeToString(sum[:]) + "  lookup.tar.gz\n"
	if err := VerifyChecksum(payload, []byte(checksums), "lookup.tar.gz"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyChecksum([]byte("tampered"), []byte(checksums), "lookup.tar.gz"); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if err := VerifyChecksum(payload, []byte(""), "lookup.tar.gz"); err == nil {
		t.Fatal("missing checksum accepted")
	}
}

func splitPlatform(value string) [2]string {
	for i := range value {
		if value[i] == '/' {
			return [2]string{value[:i], value[i+1:]}
		}
	}
	return [2]string{}
}

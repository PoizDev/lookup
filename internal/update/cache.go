package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const CheckInterval = 24 * time.Hour
const NotificationCooldown = 7 * 24 * time.Hour

type Cache struct {
	CheckedAt           time.Time `json:"checked_at"`
	LatestVersion       string    `json:"latest_version"`
	LastNotifiedVersion string    `json:"last_notified_version,omitempty"`
	LastNotifiedAt      time.Time `json:"last_notified_at,omitempty"`
}
type CacheStore struct {
	Path string
	Now  func() time.Time
}

func DefaultCachePath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "lookup", "update.json"), nil
}
func (s CacheStore) load() (Cache, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return Cache{}, err
	}
	var state Cache
	err = json.Unmarshal(data, &state)
	return state, err
}
func (s CacheStore) LoadFresh(maxAge time.Duration) (Cache, bool) {
	state, err := s.load()
	if err != nil {
		return Cache{}, false
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	return state, !state.CheckedAt.IsZero() && now.Sub(state.CheckedAt) >= 0 && now.Sub(state.CheckedAt) < maxAge
}
func (s CacheStore) Load() Cache { state, _ := s.load(); return state }
func (s CacheStore) Save(state Cache) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.Path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(s.Path, 0o600)
}
func ShouldNotify(state Cache, latest string, now time.Time, cooldown time.Duration) bool {
	return state.LastNotifiedVersion != latest || state.LastNotifiedAt.IsZero() || now.Sub(state.LastNotifiedAt) >= cooldown
}

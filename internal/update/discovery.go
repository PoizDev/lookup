package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const releasesAPI = "https://api.github.com/repos/poizdev/lookup/releases?per_page=30"

type ReleaseClient struct {
	HTTP             *http.Client
	APIBase, Version string
}
type releaseResponse struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

func NewReleaseClient(version string, timeout time.Duration) ReleaseClient {
	return ReleaseClient{HTTP: &http.Client{Timeout: timeout}, APIBase: releasesAPI, Version: version}
}
func (c ReleaseClient) Latest(ctx context.Context, includePrerelease bool) (string, error) {
	if c.HTTP == nil {
		return "", fmt.Errorf("release HTTP client is not configured")
	}
	endpoint := c.APIBase
	if endpoint == "" {
		endpoint = releasesAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Lookup/"+c.Version)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("check GitHub Releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub Releases returned HTTP %d", resp.StatusCode)
	}
	var releases []releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("decode GitHub Releases response: %w", err)
	}
	latest := ""
	for _, release := range releases {
		if release.Draft || release.TagName == "" || (!includePrerelease && (release.Prerelease || IsPrerelease(release.TagName))) {
			continue
		}
		if _, err := parseVersion(release.TagName); err != nil {
			continue
		}
		if latest == "" {
			latest = release.TagName
			continue
		}
		cmp, _ := Compare(latest, release.TagName)
		if cmp < 0 {
			latest = release.TagName
		}
	}
	if latest == "" {
		return "", fmt.Errorf("GitHub Releases returned no matching release")
	}
	return latest, nil
}

type CheckResult struct {
	Current, Latest string
	Available       bool
}
type Checker struct {
	Client ReleaseClient
	Cache  CacheStore
	Now    func() time.Time
}

func (c Checker) Check(ctx context.Context, prerelease, fresh bool) (CheckResult, error) {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	latest := ""
	if !fresh && !prerelease {
		if cached, ok := c.Cache.LoadFresh(CheckInterval); ok {
			latest = cached.LatestVersion
		}
	}
	if latest == "" {
		var err error
		latest, err = c.Client.Latest(ctx, prerelease)
		if err != nil {
			return CheckResult{}, err
		}
		state := c.Cache.Load()
		state.CheckedAt = now
		state.LatestVersion = latest
		_ = c.Cache.Save(state)
	}
	current := c.Client.Version
	cmp, err := Compare(current, latest)
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{Current: "v" + trimV(current), Latest: latest, Available: cmp < 0}, nil
}
func trimV(value string) string {
	if len(value) > 0 && value[0] == 'v' {
		return value[1:]
	}
	return value
}

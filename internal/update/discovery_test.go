package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestReleaseDiscoveryChannelsAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Lookup/0.1.0" {
			t.Errorf("User-Agent=%q", r.Header.Get("User-Agent"))
		}
		fmt.Fprint(w, `[{"tag_name":"v9.0.0-beta.1","prerelease":false},{"tag_name":"v0.2.0-rc.1","prerelease":true},{"tag_name":"v0.1.2","prerelease":false},{"tag_name":"v0.1.1","prerelease":false}]`)
	}))
	defer server.Close()
	client := ReleaseClient{HTTP: server.Client(), APIBase: server.URL, Version: "0.1.0"}
	stable, err := client.Latest(t.Context(), false)
	if err != nil || stable != "v0.1.2" {
		t.Fatalf("stable=(%q,%v)", stable, err)
	}
	pre, err := client.Latest(t.Context(), true)
	if err != nil || pre != "v9.0.0-beta.1" {
		t.Fatalf("prerelease=(%q,%v)", pre, err)
	}

	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
			defer s.Close()
			if _, err := (ReleaseClient{HTTP: s.Client(), APIBase: s.URL, Version: "0.1.0"}).Latest(t.Context(), false); err == nil {
				t.Fatal("HTTP failure accepted")
			}
		})
	}
}

func TestCachedDiscoveryAvoidsNetworkWithinInterval(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `[{"tag_name":"v0.1.2"}]`)
	}))
	defer server.Close()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := CacheStore{Path: filepath.Join(t.TempDir(), "update.json"), Now: func() time.Time { return now }}
	checker := Checker{Client: ReleaseClient{HTTP: server.Client(), APIBase: server.URL, Version: "0.1.0"}, Cache: store, Now: func() time.Time { return now }}
	if _, err := checker.Check(context.Background(), false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := checker.Check(context.Background(), false, false); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d, want 1", requests.Load())
	}
	if _, err := checker.Check(context.Background(), false, true); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("fresh check requests=%d, want 2", requests.Load())
	}
}

func TestReleaseDiscoveryRejectsInvalidResponseAndHonorsTimeout(t *testing.T) {
	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "not-json") }))
	defer invalid.Close()
	if _, err := (ReleaseClient{HTTP: invalid.Client(), APIBase: invalid.URL, Version: "0.1.0"}).Latest(t.Context(), false); err == nil {
		t.Fatal("invalid response accepted")
	}

	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer blocked.Close()
	client := ReleaseClient{HTTP: &http.Client{Timeout: 20 * time.Millisecond}, APIBase: blocked.URL, Version: "0.1.0"}
	if _, err := client.Latest(t.Context(), false); err == nil {
		t.Fatal("timeout accepted")
	}
}

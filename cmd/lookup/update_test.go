package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/poizdev/lookup/internal/config"
	"github.com/poizdev/lookup/internal/reporter"
	"github.com/poizdev/lookup/internal/ui"
	lookupupdate "github.com/poizdev/lookup/internal/update"
)

func TestJSONAndSARIFStayParseableWhenNotificationUsesStderr(t *testing.T) {
	for _, format := range []string{"json", "sarif"} {
		t.Run(format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			result := &reporter.Result{RepoPath: t.TempDir(), ToolVersion: "0.1.0"}
			if err := renderResult(config.OutputConfig{Format: format}, result, &stdout); err != nil {
				t.Fatal(err)
			}
			channel := make(chan passiveResult, 1)
			channel <- passiveResult{result: lookupupdate.CheckResult{Current: "v0.1.0", Latest: "v0.1.1", Available: true}, store: lookupupdate.CacheStore{Path: t.TempDir() + "/update.json"}}
			close(channel)
			finishPassiveUpdateCheck(channel, &stderr)
			var decoded any
			if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
				t.Fatalf("%s stdout invalid: %v\n%s", format, err, stdout.String())
			}
			if stdout.Len() == 0 || !strings.Contains(stderr.String(), "v0.1.1") {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

type fakeChecker struct {
	result lookupupdate.CheckResult
	err    error
	calls  int
}

func TestPassiveNotificationWritesOnlyToDiagnosticWriter(t *testing.T) {
	store := lookupupdate.CacheStore{Path: t.TempDir() + "/update.json", Now: func() time.Time { return time.Now() }}
	channel := make(chan passiveResult, 1)
	channel <- passiveResult{result: lookupupdate.CheckResult{Current: "v0.1.0", Latest: "v0.1.1", Available: true}, store: store}
	close(channel)
	var stdout, stderr bytes.Buffer
	finishPassiveUpdateCheck(channel, &stderr)
	if stdout.Len() != 0 {
		t.Fatalf("stdout contaminated: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Lookup v0.1.1 available") {
		t.Fatalf("stderr=%q", stderr.String())
	}
	channel = make(chan passiveResult, 1)
	channel <- passiveResult{result: lookupupdate.CheckResult{Current: "v0.1.0", Latest: "v0.1.1", Available: true}, store: store}
	close(channel)
	stderr.Reset()
	finishPassiveUpdateCheck(channel, &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("cooldown notification=%q", stderr.String())
	}
}

func (f *fakeChecker) Check(context.Context, bool, bool) (lookupupdate.CheckResult, error) {
	f.calls++
	return f.result, f.err
}

type fakeUpdater struct {
	result lookupupdate.UpdateResult
	err    error
	calls  int
}

func (f *fakeUpdater) Apply(context.Context, string) (lookupupdate.UpdateResult, error) {
	f.calls++
	return f.result, f.err
}

func TestUpdateCheckNeverChangesBinary(t *testing.T) {
	checker := &fakeChecker{result: lookupupdate.CheckResult{Current: "v0.1.0", Latest: "v0.1.1", Available: true}}
	updater := &fakeUpdater{}
	cmd := newUpdateCommand("0.1.0", checker, updater)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--check"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if updater.calls != 0 {
		t.Fatal("--check invoked updater")
	}
	if !strings.Contains(out.String(), "Update available") || !strings.Contains(out.String(), "v0.1.1") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestUpdateAppliesOnlyAfterExplicitCommand(t *testing.T) {
	checker := &fakeChecker{result: lookupupdate.CheckResult{Current: "v0.1.0", Latest: "v0.1.1", Available: true}}
	updater := &fakeUpdater{result: lookupupdate.UpdateResult{Artifact: "lookup_0.1.1_linux_amd64_musl.tar.gz"}}
	cmd := newUpdateCommand("0.1.0", checker, updater)
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if updater.calls != 1 {
		t.Fatalf("updater calls=%d", updater.calls)
	}
	if !strings.Contains(out.String(), "SHA-256 verified") || !strings.Contains(out.String(), "Lookup is now v0.1.1") {
		t.Fatalf("output=%q", out.String())
	}
}

func TestPassiveUpdatePolicyProtectsOutputModes(t *testing.T) {
	getenv := func(key string) string { return "" }
	tty := ui.Capability{IsTTY: true}
	for _, args := range [][]string{{".", "--output", "json"}, {".", "--output", "sarif"}, {"version"}} {
		if !passiveUpdateEligible(args, "0.1.0", tty, getenv) {
			t.Errorf("interactive args %v suppressed", args)
		}
	}
	for _, args := range [][]string{{"completion", "zsh"}, {"update"}, {"version", "--short"}} {
		if passiveUpdateEligible(args, "0.1.0", tty, getenv) {
			t.Errorf("args %v eligible", args)
		}
	}
	if passiveUpdateEligible([]string{"."}, "dev", tty, getenv) {
		t.Fatal("development build eligible")
	}
	if passiveUpdateEligible([]string{"."}, "0.1.0", ui.Capability{}, getenv) {
		t.Fatal("non-TTY eligible")
	}
	if passiveUpdateEligible([]string{"."}, "0.1.0", tty, func(key string) string {
		if key == "CI" {
			return "true"
		}
		return ""
	}) {
		t.Fatal("CI eligible")
	}
	if passiveUpdateEligible([]string{"."}, "0.1.0", tty, func(key string) string {
		if key == "LOOKUP_NO_UPDATE_CHECK" {
			return "1"
		}
		return ""
	}) {
		t.Fatal("opt-out ignored")
	}
}

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/poizdev/lookup/internal/ui"
	lookupupdate "github.com/poizdev/lookup/internal/update"
)

type updateChecker interface {
	Check(context.Context, bool, bool) (lookupupdate.CheckResult, error)
}
type binaryUpdater interface {
	Apply(context.Context, string) (lookupupdate.UpdateResult, error)
}
type unavailableUpdater struct{ err error }

func (u unavailableUpdater) Apply(context.Context, string) (lookupupdate.UpdateResult, error) {
	return lookupupdate.UpdateResult{}, u.err
}

func newUpdateCommand(current string, checker updateChecker, updater binaryUpdater) *cobra.Command {
	var checkOnly, prerelease bool
	cmd := &cobra.Command{Use: "update", Short: "Check for and install Lookup updates", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if !lookupupdate.IsReleaseVersion(current) {
			return fmt.Errorf("update is unavailable for development builds")
		}
		result, err := checker.Check(cmd.Context(), prerelease, true)
		if err != nil {
			return fmt.Errorf("check for updates: %w", err)
		}
		if checkOnly {
			fmt.Fprintf(cmd.OutOrStdout(), "Current:  %s\nLatest:   %s\n\n", result.Current, result.Latest)
			if result.Available {
				fmt.Fprintln(cmd.OutOrStdout(), "Update available.\nRun `lookup update` to upgrade.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Lookup is up to date.")
			}
			return nil
		}
		if !result.Available {
			fmt.Fprintln(cmd.OutOrStdout(), "Lookup is already up to date.")
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Lookup Update\n\nCurrent   %s\nLatest    %s\n\n", result.Current, result.Latest)
		updated, err := updater.Apply(cmd.Context(), result.Latest)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Downloaded %s\n✓ SHA-256 verified\n", updated.Artifact)
		if updated.Staged {
			fmt.Fprintln(cmd.OutOrStdout(), "✓ Update staged; replacement will complete after Lookup exits.")
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Updated successfully\n\nLookup is now %s\n", result.Latest)
		return nil
	}}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Check for updates without changing the executable")
	cmd.Flags().BoolVar(&prerelease, "prerelease", false, "Include prerelease versions")
	return cmd
}

func defaultUpdateCommand(current string) *cobra.Command {
	cachePath, cacheErr := lookupupdate.DefaultCachePath()
	receiptPath, receiptErr := lookupupdate.DefaultReceiptPath()
	if cacheErr != nil {
		cachePath = ""
	}
	client := lookupupdate.NewReleaseClient(current, 10*time.Second)
	checker := &lookupupdate.Checker{Client: client, Cache: lookupupdate.CacheStore{Path: cachePath}}
	var updater binaryUpdater = &lookupupdate.Updater{HTTP: &http.Client{Timeout: 30 * time.Second}, ReceiptPath: receiptPath, Version: current, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if receiptErr != nil {
		updater = unavailableUpdater{err: receiptErr}
	}
	return newUpdateCommand(current, checker, updater)
}

type passiveResult struct {
	result lookupupdate.CheckResult
	store  lookupupdate.CacheStore
}

func passiveUpdateEligible(args []string, current string, cap ui.Capability, getenv func(string) string) bool {
	if !cap.IsTTY || !lookupupdate.IsReleaseVersion(current) || getenv("LOOKUP_NO_UPDATE_CHECK") == "1" {
		return false
	}
	for _, key := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "TF_BUILD", "BUILDKITE", "JENKINS_URL"} {
		if getenv(key) != "" {
			return false
		}
	}
	joined := " " + strings.Join(args, " ") + " "
	if strings.Contains(joined, " completion ") || strings.HasPrefix(joined, " completion ") || strings.Contains(joined, " update ") || strings.HasPrefix(joined, " update ") {
		return false
	}
	if strings.Contains(joined, " version ") && strings.Contains(joined, " --short ") {
		return false
	}
	return true
}
func startPassiveUpdateCheck(ctx context.Context, args []string, current string, stderr io.Writer) <-chan passiveResult {
	channel := make(chan passiveResult, 1)
	if !passiveUpdateEligible(args, current, ui.Detect(stderr), os.Getenv) {
		close(channel)
		return channel
	}
	path, err := lookupupdate.DefaultCachePath()
	if err != nil {
		close(channel)
		return channel
	}
	store := lookupupdate.CacheStore{Path: path}
	checker := lookupupdate.Checker{Client: lookupupdate.NewReleaseClient(current, 1500*time.Millisecond), Cache: store}
	go func() {
		defer close(channel)
		result, err := checker.Check(ctx, false, false)
		if err == nil {
			channel <- passiveResult{result: result, store: store}
		}
	}()
	return channel
}
func finishPassiveUpdateCheck(channel <-chan passiveResult, stderr io.Writer) {
	select {
	case passive, ok := <-channel:
		if !ok || !passive.result.Available {
			return
		}
		now := time.Now()
		state := passive.store.Load()
		if !lookupupdate.ShouldNotify(state, passive.result.Latest, now, lookupupdate.NotificationCooldown) {
			return
		}
		fmt.Fprintf(stderr, "◇ Lookup %s available · `lookup update`\n", passive.result.Latest)
		state.LastNotifiedVersion = passive.result.Latest
		state.LastNotifiedAt = now
		_ = passive.store.Save(state)
	default:
	}
}

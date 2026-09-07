package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsUpdaterStagesVerifiedBinaryAndStartsHelper(t *testing.T) {
	archive := zipBinary(t, []byte("windows executable"))
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "checksums.txt" {
			fmt.Fprintf(w, "%x  lookup_0.1.1_windows_amd64.zip\n", sum)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "lookup.exe")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(dir, "install.json")
	_ = WriteReceipt(receipt, target)
	started := false
	u := Updater{HTTP: server.Client(), ReleaseBase: server.URL, ReceiptPath: receipt, Executable: func() (string, error) { return target, nil }, GOOS: "windows", GOARCH: "amd64", Validate: func(context.Context, string, string) error { return nil }, StartProcess: func(string, ...string) error { started = true; return nil }}
	result, err := u.Apply(context.Background(), "v0.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Staged || !started {
		t.Fatalf("result=%+v started=%v", result, started)
	}
	if data, _ := os.ReadFile(target); string(data) != "old" {
		t.Fatalf("running executable changed early: %q", data)
	}
	if data, err := os.ReadFile(target + ".update.exe"); err != nil || string(data) != "windows executable" {
		t.Fatalf("staged=(%q,%v)", data, err)
	}
}

func TestUpdaterVerifiesAndReplacesManagedUnixBinary(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Unix replacement test")
	}
	archive := tarBinary(t, []byte("#!/bin/sh\necho 0.1.1\n"))
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "checksums.txt" {
			fmt.Fprintf(w, "%x  lookup_0.1.1_linux_amd64_musl.tar.gz\n", sum)
			return
		}
		w.Write(archive)
	}))
	defer server.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "lookup")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho 0.1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(dir, "install.json")
	if err := WriteReceipt(receipt, target); err != nil {
		t.Fatal(err)
	}
	u := Updater{HTTP: server.Client(), ReleaseBase: server.URL, ReceiptPath: receipt, Executable: func() (string, error) { return target, nil }, GOOS: "linux", GOARCH: "amd64"}
	result, err := u.Apply(context.Background(), "v0.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Artifact != "lookup_0.1.1_linux_amd64_musl.tar.gz" {
		t.Fatalf("artifact=%q", result.Artifact)
	}
	data, _ := os.ReadFile(target)
	if !bytes.Contains(data, []byte("0.1.1")) {
		t.Fatalf("binary not replaced: %q", data)
	}
}

func TestChecksumFailurePreservesInstalledBinary(t *testing.T) {
	archive := tarBinary(t, []byte("#!/bin/sh\necho 0.1.1\n"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "checksums.txt" {
			fmt.Fprint(w, "deadbeef  lookup_0.1.1_linux_amd64_musl.tar.gz\n")
			return
		}
		w.Write(archive)
	}))
	defer server.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "lookup")
	old := []byte("#!/bin/sh\necho 0.1.0\n")
	if err := os.WriteFile(target, old, 0o755); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(dir, "install.json")
	_ = WriteReceipt(receipt, target)
	u := Updater{HTTP: server.Client(), ReleaseBase: server.URL, ReceiptPath: receipt, Executable: func() (string, error) { return target, nil }, GOOS: "linux", GOARCH: "amd64"}
	if _, err := u.Apply(context.Background(), "v0.1.1"); err == nil {
		t.Fatal("checksum failure accepted")
	}
	data, _ := os.ReadFile(target)
	if !bytes.Equal(data, old) {
		t.Fatalf("installed binary changed: %q", data)
	}
}

func tarBinary(t *testing.T, payload []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "lookup", Mode: 0o755, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func zipBinary(t *testing.T, payload []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	file, err := zw.Create("lookup.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

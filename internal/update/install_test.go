package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialInstallerReceiptRequiresMatchingExecutable(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "lookup")
	if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(dir, "state", "install.json")
	if err := WriteReceipt(receiptPath, executable); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManagedReceipt(receiptPath, executable); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManagedReceipt(receiptPath, filepath.Join(dir, "other")); err == nil {
		t.Fatal("wrong executable path accepted")
	}
	if _, err := LoadManagedReceipt(filepath.Join(dir, "missing.json"), executable); err == nil || !strings.Contains(err.Error(), "official installer") {
		t.Fatalf("missing receipt error=%v", err)
	}
}

func TestUnixAtomicReplacementPreservesOldBinaryOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "lookup")
	candidate := filepath.Join(dir, "candidate")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceUnix(target, candidate); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "new" {
		t.Fatalf("target=(%q,%v)", data, err)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	if err := ReplaceUnix(filepath.Join(dir, "missing", "lookup"), candidate); err == nil {
		t.Fatal("invalid target replacement succeeded")
	}
}

func TestWindowsReplacementIsStagedUntilCurrentProcessExits(t *testing.T) {
	name, args := WindowsHelperCommand(42, `C:\Lookup\lookup.exe`, `C:\Lookup\lookup.update.exe`)
	if name != "powershell.exe" {
		t.Fatalf("helper=%q", name)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"Wait-Process", "42", "lookup.update.exe", "lookup.exe"} {
		if !strings.Contains(joined, want) {
			t.Errorf("helper args missing %q: %s", want, joined)
		}
	}
}

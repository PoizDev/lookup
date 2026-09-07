package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const Repository = "poizdev/lookup"
const InstallMethod = "official-installer"

type Receipt struct {
	Method      string `json:"method"`
	InstallPath string `json:"install_path"`
	Repository  string `json:"repository"`
}

func DefaultReceiptPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "lookup", "install.json"), nil
}
func WriteReceipt(path, executable string) error {
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return err
	}
	receipt := Receipt{Method: InstallMethod, InstallPath: filepath.Clean(absolute), Repository: Repository}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
func LoadManagedReceipt(path, executable string) (Receipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Receipt{}, fmt.Errorf("Lookup does not appear to be managed by the official installer: %w", err)
	}
	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return Receipt{}, fmt.Errorf("invalid installation receipt: %w", err)
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return Receipt{}, err
	}
	pathsMatch := filepath.Clean(receipt.InstallPath) == filepath.Clean(absolute)
	if runtime.GOOS == "windows" {
		pathsMatch = strings.EqualFold(filepath.Clean(receipt.InstallPath), filepath.Clean(absolute))
	}
	if receipt.Method != InstallMethod || receipt.Repository != Repository || !pathsMatch {
		return Receipt{}, fmt.Errorf("Lookup does not appear to be managed by the official installer")
	}
	return receipt, nil
}

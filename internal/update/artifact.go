package update

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func ArtifactName(version, goos, goarch string) (string, error) {
	v, err := parseVersion(version)
	if err != nil {
		return "", err
	}
	_ = v
	if goarch != "amd64" && goarch != "arm64" {
		return "", fmt.Errorf("unsupported architecture %s", goarch)
	}
	target := goos + "_" + goarch
	ext := "tar.gz"
	switch goos {
	case "linux":
		target += "_musl"
	case "darwin":
	case "windows":
		if goarch != "amd64" {
			return "", fmt.Errorf("unsupported Windows architecture %s", goarch)
		}
		ext = "zip"
	default:
		return "", fmt.Errorf("unsupported operating system %s", goos)
	}
	return fmt.Sprintf("lookup_%s_%s.%s", strings.TrimPrefix(version, "v"), target, ext), nil
}
func VerifyChecksum(payload, manifest []byte, filename string) error {
	want := ""
	scanner := bufio.NewScanner(bytes.NewReader(manifest))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && strings.TrimPrefix(fields[1], "*") == filename {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksum entry not found for %s", filename)
	}
	sum := sha256.Sum256(payload)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s", filename)
	}
	return nil
}

package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxDownloadSize = 128 << 20

type UpdateResult struct {
	Artifact string
	Staged   bool
}
type Updater struct {
	HTTP                              *http.Client
	ReleaseBase, ReceiptPath, Version string
	Executable                        func() (string, error)
	GOOS, GOARCH                      string
	StartProcess                      func(string, ...string) error
	Validate                          func(context.Context, string, string) error
}

func (u Updater) Apply(ctx context.Context, latest string) (UpdateResult, error) {
	executable := u.Executable
	if executable == nil {
		executable = os.Executable
	}
	target, err := executable()
	if err != nil {
		return UpdateResult{}, err
	}
	if _, err = LoadManagedReceipt(u.ReceiptPath, target); err != nil {
		return UpdateResult{}, fmt.Errorf("%w\n\nUpdate using the package manager that installed Lookup, or reinstall from GitHub Releases.", err)
	}
	archive, err := ArtifactName(latest, u.GOOS, u.GOARCH)
	if err != nil {
		return UpdateResult{}, err
	}
	base := u.ReleaseBase
	if base == "" {
		base = "https://github.com/" + Repository + "/releases/download/" + latest
	}
	payload, err := u.download(ctx, base+"/"+archive)
	if err != nil {
		return UpdateResult{}, err
	}
	manifest, err := u.download(ctx, base+"/checksums.txt")
	if err != nil {
		return UpdateResult{}, err
	}
	if err = VerifyChecksum(payload, manifest, archive); err != nil {
		return UpdateResult{}, err
	}
	temp, err := os.MkdirTemp("", "lookup-update-*")
	if err != nil {
		return UpdateResult{}, err
	}
	defer os.RemoveAll(temp)
	candidate, err := extractBinary(payload, archive, temp)
	if err != nil {
		return UpdateResult{}, err
	}
	if u.GOOS != "windows" {
		if err = os.Chmod(candidate, 0o755); err != nil {
			return UpdateResult{}, err
		}
	}
	validator := u.Validate
	if validator == nil {
		validator = validateCandidate
	}
	if err = validator(ctx, candidate, latest); err != nil {
		return UpdateResult{}, err
	}
	if u.GOOS == "windows" {
		staged := target + ".update.exe"
		if err = copyFile(candidate, staged, 0o755); err != nil {
			return UpdateResult{}, err
		}
		name, args := WindowsHelperCommand(os.Getpid(), target, staged)
		starter := u.StartProcess
		if starter == nil {
			starter = startDetached
		}
		if err = starter(name, args...); err != nil {
			os.Remove(staged)
			return UpdateResult{}, fmt.Errorf("start Windows update helper: %w", err)
		}
		return UpdateResult{Artifact: archive, Staged: true}, nil
	}
	if err = ReplaceUnix(target, candidate); err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{Artifact: archive}, nil
}

func validateCandidate(ctx context.Context, candidate, version string) error {
	command := exec.CommandContext(ctx, candidate, "version", "--short")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("validate downloaded executable: %w", err)
	}
	if strings.TrimSpace(string(output)) != trimV(version) {
		return fmt.Errorf("downloaded executable version mismatch")
	}
	return nil
}
func (u Updater) download(ctx context.Context, url string) ([]byte, error) {
	if u.HTTP == nil {
		return nil, fmt.Errorf("update HTTP client is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Lookup/"+trimV(u.Version))
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", filepath.Base(url), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", filepath.Base(url), resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDownloadSize {
		return nil, fmt.Errorf("download exceeds size limit")
	}
	return data, nil
}
func extractBinary(payload []byte, archive, destination string) (string, error) {
	name := "lookup"
	if strings.HasSuffix(archive, ".zip") {
		name = "lookup.exe"
		reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
		if err != nil {
			return "", err
		}
		for _, file := range reader.File {
			if filepath.Base(file.Name) != name {
				continue
			}
			source, err := file.Open()
			if err != nil {
				return "", err
			}
			defer source.Close()
			target := filepath.Join(destination, name)
			if err = writeReader(target, source, 0o755); err != nil {
				return "", err
			}
			return target, nil
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return "", err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			if filepath.Base(header.Name) != name || header.Typeflag != tar.TypeReg {
				continue
			}
			target := filepath.Join(destination, name)
			if err = writeReader(target, tr, 0o755); err != nil {
				return "", err
			}
			return target, nil
		}
	}
	return "", fmt.Errorf("archive does not contain %s", name)
}
func writeReader(path string, source io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, source)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
func copyFile(source, target string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
func startDetached(name string, args ...string) error { return exec.Command(name, args...).Start() }

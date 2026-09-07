package update

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func ReplaceUnix(target, candidate string) error {
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("inspect installed executable: %w", err)
	}
	source, err := os.Open(candidate)
	if err != nil {
		return err
	}
	defer source.Close()
	temp, err := os.CreateTemp(filepath.Dir(target), ".lookup-update-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		temp.Close()
		if cleanup {
			os.Remove(tempPath)
		}
	}()
	if _, err = io.Copy(temp, source); err != nil {
		return err
	}
	if err = temp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tempPath, target); err != nil {
		return fmt.Errorf("replace installed executable: %w", err)
	}
	cleanup = false
	return nil
}

func WindowsHelperCommand(parentPID int, target, staged string) (string, []string) {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	backup := target + ".previous"
	script := "$ErrorActionPreference='Stop'; Wait-Process -Id " + strconv.Itoa(parentPID) + "; $target=" + quote(target) + "; $staged=" + quote(staged) + "; $backup=" + quote(backup) + "; Remove-Item -LiteralPath $backup -Force -ErrorAction SilentlyContinue; try { Move-Item -LiteralPath $target -Destination $backup -Force; Move-Item -LiteralPath $staged -Destination $target -Force; Remove-Item -LiteralPath $backup -Force } catch { if ((Test-Path -LiteralPath $backup) -and -not (Test-Path -LiteralPath $target)) { Move-Item -LiteralPath $backup -Destination $target -Force }; throw }"
	return "powershell.exe", []string{"-NoProfile", "-WindowStyle", "Hidden", "-Command", script}
}

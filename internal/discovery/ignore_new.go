package discovery

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

type Ignorer struct {
	matcher gitignore.Matcher
}

func NewIgnorer(root string) (*Ignorer, error) {
	ignoreFiles := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() && entry.Name() == ".gitignore" {
			ignoreFiles = append(ignoreFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(ignoreFiles, func(i, j int) bool {
		left := pathDepth(root, ignoreFiles[i])
		right := pathDepth(root, ignoreFiles[j])
		if left != right {
			return left < right
		}
		return ignoreFiles[i] < ignoreFiles[j]
	})

	patterns := []gitignore.Pattern{gitignore.ParsePattern(".git/", nil)}
	for _, path := range ignoreFiles {
		domain, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		if domain == "." {
			domain = ""
		}
		loaded, err := readIgnorePatterns(path, splitGitPath(domain))
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, loaded...)
	}
	return &Ignorer{matcher: gitignore.NewMatcher(patterns)}, nil
}

func readIgnorePatterns(path string, domain []string) ([]gitignore.Pattern, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	patterns := make([]gitignore.Pattern, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, gitignore.ParsePattern(line, domain))
	}
	return patterns, scanner.Err()
}

func (ig *Ignorer) IsIgnored(path string, isDir bool) bool {
	return ig.matcher.Match(splitGitPath(path), isDir)
}

func splitGitPath(path string) []string {
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." || path == "" {
		return nil
	}
	return strings.Split(strings.Trim(path, "/"), "/")
}

func pathDepth(root, path string) int {
	relative, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || relative == "." {
		return 0
	}
	return len(splitGitPath(relative))
}

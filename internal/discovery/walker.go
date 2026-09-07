package discovery

import (
	"io/fs"
	"path/filepath"
	"strings"
)

type DiscoveredFile struct {
	Path     string
	RelPath  string
	Size     int64
	Language string
}

type Stats struct {
	TotalFiles      int
	SkippedFiles    int
	SkippedBySize   int
	SkippedByIgnore int
	SkippedByTest   int
	ByLanguage      map[string]int
}

type Walker struct {
	root         string
	skipDirs     []string
	maxFileKB    int
	ignorer      *Ignorer
	classifier   *Classifier
	includeTests bool
}

func NewWalker(root string, skipDirs []string, maxFileKB int, includeTests bool) (*Walker, error) {
	ignorer, err := NewIgnorer(root)
	if err != nil {
		return nil, err
	}
	return &Walker{
		root:         root,
		skipDirs:     skipDirs,
		maxFileKB:    maxFileKB,
		ignorer:      ignorer,
		classifier:   NewClassifier(),
		includeTests: includeTests,
	}, nil
}

func (w *Walker) Walk() ([]DiscoveredFile, *Stats, error) {
	var files []DiscoveredFile
	stats := &Stats{
		ByLanguage: make(map[string]int),
	}

	err := filepath.WalkDir(w.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(w.root, path)
		if err != nil {
			return err
		}

		isDir := d.IsDir()

		// Check explicit skip dirs
		if isDir {
			for _, skipDir := range w.skipDirs {
				if d.Name() == skipDir {
					return filepath.SkipDir
				}
			}
		}

		// Check ignorer (.gitignore rules)
		if w.ignorer.IsIgnored(relPath, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			stats.SkippedFiles++
			stats.SkippedByIgnore++
			return nil
		}

		if isDir {
			return nil
		}

		// If we find a nested .gitignore, parse it dynamically
		if d.Name() == ".gitignore" {
			// Skip adding .gitignore as a source file
			stats.SkippedFiles++
			return nil
		}

		if !w.includeTests && strings.HasSuffix(d.Name(), "_test.go") {
			stats.SkippedFiles++
			stats.SkippedByTest++
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		// Check file size
		if info.Size() > int64(w.maxFileKB)*1024 {
			stats.SkippedFiles++
			stats.SkippedBySize++
			return nil
		}

		// Classify file language
		lang := w.classifier.Classify(path)
		if lang == "" {
			stats.SkippedFiles++
			return nil
		}

		stats.TotalFiles++
		stats.ByLanguage[lang]++

		files = append(files, DiscoveredFile{
			Path:     path,
			RelPath:  relPath,
			Size:     info.Size(),
			Language: lang,
		})

		return nil
	})

	return files, stats, err
}

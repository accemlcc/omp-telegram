package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

func resolveWorkspace(root, input string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New("The workspace root must be an absolute path.")
	}
	if strings.IndexFunc(input, unicode.IsControl) >= 0 {
		return "", errors.New("Workspace paths cannot contain control characters.")
	}
	if !filepath.IsAbs(input) && !strings.HasPrefix(input, "~") {
		if strings.TrimSpace(input) == "" || input == "." || input == ".." || strings.ContainsAny(input, "/\\") {
			return "", errors.New("Use a single workspace name, an absolute project path, or ~/path.")
		}
		input = filepath.Join(root, input)
		if info, err := os.Lstat(input); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("A named workspace cannot be a symbolic link.")
			}
		} else if !os.IsNotExist(err) {
			return "", errors.New("Cannot access the named workspace.")
		}
	} else if input == "~" || strings.HasPrefix(input, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || !filepath.IsAbs(home) {
			return "", errors.New("Cannot determine an absolute user home directory.")
		}
		input = filepath.Join(home, strings.TrimPrefix(input, "~"))
	}
	if !filepath.IsAbs(input) {
		return "", errors.New("Use a workspace name, an absolute project path, or ~/path.")
	}

	// Resolve the existing ancestor, then retain missing components without
	// creating anything. Lstat distinguishes dangling links from missing paths.
	path := filepath.Clean(input)
	ancestor := path
	for {
		if _, err := os.Lstat(ancestor); err != nil {
			if !os.IsNotExist(err) {
				return "", errors.New("Cannot access the project path.")
			}
			parent := filepath.Dir(ancestor)
			if parent == ancestor {
				return "", errors.New("Cannot resolve the project path.")
			}
			ancestor = parent
			continue
		}
		canonical, err := filepath.EvalSymlinks(ancestor)
		if err != nil {
			return "", errors.New("Cannot resolve the project path.")
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.IsDir() {
			return "", errors.New("The project path must be an accessible directory.")
		}
		remainder, err := filepath.Rel(ancestor, path)
		if err != nil {
			return "", errors.New("Cannot resolve the project path.")
		}
		return filepath.Join(canonical, remainder), nil
	}
}

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
		input = root + string(filepath.Separator) + input
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
		input = home + strings.TrimPrefix(input, "~")
	}
	if !filepath.IsAbs(input) {
		return "", errors.New("Use a workspace name, an absolute project path, or ~/path.")
	}

	resolved := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(input, string(filepath.Separator)), string(filepath.Separator))
	for i, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		candidate := filepath.Join(resolved, part)
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			return resolved + string(filepath.Separator) + strings.Join(parts[i:], string(filepath.Separator)), nil
		}
		if err != nil {
			return "", errors.New("Cannot access the project path.")
		}
		if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return "", errors.New("The project path must be an accessible directory.")
		}
		resolved, err = filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", errors.New("Cannot resolve the project path.")
		}
		info, err = os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return "", errors.New("The project path must be an accessible directory.")
		}
	}
	return resolved, nil
}

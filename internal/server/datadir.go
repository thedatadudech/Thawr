package server

import (
	"fmt"
	"os"
)

// ensureDataDir creates dir with mode 0700 or verifies an existing one
// is not group- or world-writable.
func ensureDataDir(dir string) (created bool, err error) {
	fi, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return false, fmt.Errorf("server: create data_dir %s: %w", dir, err)
		}
		return true, nil
	case err != nil:
		return false, fmt.Errorf("server: stat data_dir %s: %w", dir, err)
	case !fi.IsDir():
		return false, fmt.Errorf("server: data_dir %s is not a directory", dir)
	}
	if err := checkDirPerms(dir, fi); err != nil {
		return false, err
	}
	return false, nil
}

// writeSecretFile writes data to path with mode 0600, replacing any
// existing file atomically.
func writeSecretFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("server: write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("server: rename %s: %w", path, err)
	}
	return nil
}

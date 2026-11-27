package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/l3ony2k/twinnel/core/legacy"
	"github.com/l3ony2k/twinnel/core/privfile"
)

// The app's files from before the rename to Twinnel (it was "duet"): its
// prefs folder ~/Library/Application Support/duet and the command line
// links it made (…/bin/duet). Remove with core/legacy.

// prefsFiles move from the old prefs folder on the first launch.
var prefsFiles = []string{"app.json", "window-state.json"}

// migratePrefs moves app.json and window-state.json from configDir/duet to
// configDir/twinnel, each only when the new folder has none (a newer file
// wins). The old folder keeps the rest (install-local's backups). It
// returns the files it moved.
func migratePrefs(configDir string) ([]string, error) {
	oldDir := filepath.Join(configDir, legacy.AppSupportDir)
	newDir := filepath.Join(configDir, "twinnel")
	var moved []string
	for _, name := range prefsFiles {
		from, to := filepath.Join(oldDir, name), filepath.Join(newDir, name)
		if _, err := os.Lstat(from); err != nil {
			continue
		}
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if err := privfile.MkdirAll(newDir); err != nil { // app.json may hold the controller secret
			return moved, err
		}
		if err := os.Rename(from, to); err != nil {
			return moved, err
		}
		moved = append(moved, name)
	}
	return moved, nil
}

// legacyPrefsDir is the old prefs folder ("" when unknown).
func legacyPrefsDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, legacy.AppSupportDir)
}

// legacyLeftovers are the old prefs folder's pieces that exist (for the
// 完全移除 list).
func legacyLeftovers() bool {
	d := legacyPrefsDir()
	if d == "" {
		return false
	}
	_, err := os.Stat(d)
	return err == nil
}

// removeLegacyPrefs deletes the old prefs folder's known files and its
// backups folder, then the folder when it is empty. Only a folder named
// like the old app is touched.
func removeLegacyPrefs(dir string) error {
	if filepath.Base(dir) != legacy.AppSupportDir {
		return errors.New("not the old prefs folder: " + dir)
	}
	var errs []error
	for _, name := range prefsFiles {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if err := os.RemoveAll(filepath.Join(dir, "backups")); err != nil {
		errs = append(errs, err)
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err) // not empty: something else lives there; left alone
	}
	return errors.Join(errs...)
}

// isLegacyLink reports whether path is a command line link the old app
// made: a symlink to …/duet inside an app bundle.
func isLegacyLink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	dst, err := os.Readlink(path)
	return err == nil && filepath.Base(dst) == legacy.CLI && strings.Contains(dst, ".app/Contents/Resources/bin/")
}

package data

import (
	"os"
	"path/filepath"
	"syscall"
)

// withFileLock runs fn while holding an flock on path's sibling ".lock" file.
//
// The registry and recents files are written by more than one process — the
// TUI and `medusa workspace` — and an in-process mutex serializes neither
// against the other. A separate lock file is used rather than the data file
// itself because writes replace the data file by rename, and a lock held on a
// file that has just been renamed over protects nothing.
func withFileLock(path string, exclusive bool, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}

// writeFileAtomic replaces path with data by writing a uniquely named temp
// file beside it and renaming it into place, so a reader sees either the old
// file or the new one, never a truncated one. The temp name is unique because
// two processes writing at once must not share one.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

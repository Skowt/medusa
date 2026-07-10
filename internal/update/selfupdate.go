package update

// ReinstallCommand reinstalls medusa via the install script. It lands in
// ~/.local/bin and never escalates, so it is the correct remedy for a binary
// that was sudo-installed into a root-owned directory.
const ReinstallCommand = `curl -fsSL https://raw.githubusercontent.com/Skowt/medusa/main/install.sh | sh`

// SelfUpdateStatus describes whether medusa can replace its own binary in place.
type SelfUpdateStatus struct {
	// BinaryPath is the resolved path of the running binary.
	BinaryPath string
	// Writable reports whether an upgrade could install over BinaryPath.
	Writable bool
	// GoInstall reports whether medusa came from `go install`, which upgrades
	// through the go toolchain rather than through us.
	GoInstall bool
	// Err is set when the running binary could not be resolved at all.
	Err error
}

// Blocked reports whether an in-place upgrade is impossible because medusa
// lives somewhere the current user cannot write — typically a sudo install into
// a root-owned /usr/local/bin. `go install` builds are excluded: they cannot be
// upgraded in place either, but the remedy is different and the user is told so
// when they try. An unresolvable path is not reported as blocked, because we
// have no evidence either way and a false alarm on every launch is worse than
// staying quiet.
func (s SelfUpdateStatus) Blocked() bool {
	return s.Err == nil && !s.GoInstall && !s.Writable
}

// CheckSelfUpdate resolves the running binary and reports whether an upgrade
// could install over it.
func CheckSelfUpdate() SelfUpdateStatus {
	path, err := GetCurrentBinaryPath()
	if err != nil {
		return SelfUpdateStatus{Err: err}
	}
	return SelfUpdateStatus{
		BinaryPath: path,
		Writable:   CanWrite(path),
		GoInstall:  isGoInstallPath(path),
	}
}

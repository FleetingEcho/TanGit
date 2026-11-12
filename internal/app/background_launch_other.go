//go:build !linux && !windows

package app

// LaunchServices and the bundled macOS command already own detachment. Other
// platforms keep their existing foreground behavior.
func launchInBackground() (bool, error) {
	return false, nil
}

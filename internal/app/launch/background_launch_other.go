//go:build !linux && !windows

package launch

// LaunchServices and the bundled macOS command already own detachment. Other
// platforms keep their existing foreground behavior.
func LaunchInBackground() (bool, error) {
	return false, nil
}

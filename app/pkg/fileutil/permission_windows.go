// go:build windows
// +build windows

package fileutil

// GetUmask retrieves the current process umask safely (stub for windows)
func GetUmask() int {
	return 0
}

// ChmodWithUmask sets file/directory permissions based on umask (stub for windows)
func ChmodWithUmask(path string, isDir bool) error {
	// Chmod is a no-op / basic on Windows
	return nil
}

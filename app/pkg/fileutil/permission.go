package fileutil

import (
	"os"
	"syscall"
)

// GetUmask retrieves the current process umask safely
func GetUmask() int {
	umask := syscall.Umask(0)
	syscall.Umask(umask)
	return umask
}

// ChmodWithUmask sets file/directory permissions based on umask
func ChmodWithUmask(path string, isDir bool) error {
	umask := GetUmask()
	var mode os.FileMode
	if isDir {
		mode = os.FileMode(0777 &^ umask)
	} else {
		mode = os.FileMode(0666 &^ umask)
	}
	return os.Chmod(path, mode)
}

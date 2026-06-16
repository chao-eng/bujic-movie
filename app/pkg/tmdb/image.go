package tmdb

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// GetImageURL constructs the full TMDB image URL
// size can be: w500, w1280, original, etc.
func GetImageURL(path string, size string) string {
	if path == "" {
		return ""
	}
	if size == "" {
		size = "original"
	}
	return fmt.Sprintf("https://image.tmdb.org/t/p/%s%s", size, path)
}

// DownloadImage downloads an image from URL and saves it atomically to targetPath
func DownloadImage(url string, targetPath string) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download image, status code: %d", resp.StatusCode)
	}

	// Create a temporary file in the destination directory to ensure atomic rename on the same disk mount
	tempFile, err := os.CreateTemp(dir, "bujic-img-*.tmp")
	if err != nil {
		return err
	}
	tempName := tempFile.Name()

	// Ensure cleanup in case of panic or error before rename
	defer func() {
		if tempFile != nil {
			tempFile.Close()
			os.Remove(tempName)
		}
	}()

	// Stream file in chunks using standard buffer size
	buffer := make([]byte, 32*1024)
	if _, err = io.CopyBuffer(tempFile, resp.Body, buffer); err != nil {
		return err
	}

	if err = tempFile.Close(); err != nil {
		return err
	}
	tempFile = nil // mark as closed so the defer doesn't remove the final file

	// Atomic replace
	return os.Rename(tempName, targetPath)
}

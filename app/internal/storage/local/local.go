package local

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/bujic-movie/bujic-movie/internal/storage"
)

type LocalStorage struct{}

func NewLocalStorage() storage.Storage {
	return &LocalStorage{}
}

// List implements storage.Storage
func (l *LocalStorage) List(path string) ([]storage.FileItem, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var items []storage.FileItem
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		items = append(items, storage.FileItem{
			Path:    filepath.Join(path, entry.Name()),
			Name:    entry.Name(),
			Size:    info.Size(),
			IsDir:   entry.IsDir(),
			ModTime: info.ModTime(),
		})
	}
	return items, nil
}

// Read implements storage.Storage
func (l *LocalStorage) Read(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

// Write implements storage.Storage
func (l *LocalStorage) Write(path string, r io.Reader) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, r)
	return err
}

// Delete implements storage.Storage
func (l *LocalStorage) Delete(path string) error {
	return os.RemoveAll(path)
}

// Mkdir implements storage.Storage
func (l *LocalStorage) Mkdir(path string) error {
	return os.MkdirAll(path, 0755)
}

// Stat implements storage.Storage
func (l *LocalStorage) Stat(path string) (storage.FileItem, error) {
	info, err := os.Stat(path)
	if err != nil {
		return storage.FileItem{}, err
	}

	return storage.FileItem{
		Path:    path,
		Name:    info.Name(),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}, nil
}

// Copy implements storage.Storage
func (l *LocalStorage) Copy(src, dst string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}

	// Handle directory copy
	if srcInfo.IsDir() {
		return l.copyDir(src, dst)
	}

	// Handle symlink copy
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		linkDst, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(linkDst, dst)
	}

	// Ensure parent directory of dst exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return nil
}

func (l *LocalStorage) copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if err := l.Copy(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

// Move implements storage.Storage
func (l *LocalStorage) Move(src, dst string) error {
	// Ensure parent directory of dst exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	// Attempt standard rename
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// Check if the error is "cross-device link" (EXDEV)
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		if errno, ok := linkErr.Err.(syscall.Errno); ok && errno == syscall.EXDEV {
			// Cross-device rename fallback: Copy + Delete
			if err := l.Copy(src, dst); err != nil {
				return fmt.Errorf("cross-device copy failed: %w", err)
			}
			return l.Delete(src)
		}
	}

	return err
}

// Link implements storage.Storage
func (l *LocalStorage) Link(src, dst string) error {
	// Ensure parent directory of dst exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.Link(src, dst)
}

// Symlink implements storage.Storage
func (l *LocalStorage) Symlink(src, dst string) error {
	// Ensure parent directory of dst exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.Symlink(src, dst)
}

// Hash implements storage.Storage (SHA256 streamed)
func (l *LocalStorage) Hash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// IsDir implements storage.Storage
func (l *LocalStorage) IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsBluray implements storage.Storage
func (l *LocalStorage) IsBluray(path string) bool {
	if !l.IsDir(path) {
		return false
	}
	bdmvPath := filepath.Join(path, "BDMV")
	info, err := os.Stat(bdmvPath)
	return err == nil && info.IsDir()
}

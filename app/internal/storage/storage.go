package storage

import (
	"io"
	"time"
)

// Storage defines the unified interface for storage backends (local, cloud, etc.)
// Inspired by rclone's Fs abstraction.
type Storage interface {
	// Basic file operations
	List(path string) ([]FileItem, error)          // List directory contents
	Read(path string) (io.ReadCloser, error)       // Read file stream
	Write(path string, r io.Reader) error          // Write file from stream
	Delete(path string) error                      // Delete file or empty directory
	Mkdir(path string) error                       // Create directory
	Stat(path string) (FileItem, error)            // Get file stats

	// High-level operations
	Copy(src, dst string) error                    // Copy file/directory
	Move(src, dst string) error                    // Move file/directory
	Link(src, dst string) error                    // Create hard link
	Symlink(src, dst string) error                 // Create symbolic link

	// Metadata and utilities
	Hash(path string) (string, error)              // Calculate file hash (streamed)
	IsDir(path string) bool                        // Check if path is a directory
	IsBluray(path string) bool                     // Check if path is a Blu-ray directory (contains BDMV)
}

// FileItem holds metadata for a file or directory
type FileItem struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
}

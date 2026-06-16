package fileutil

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Supported extensions
var VideoExtensions = map[string]bool{
	".mp4":  true,
	".mkv":  true,
	".avi":  true,
	".mov":  true,
	".wmv":  true,
	".flv":  true,
	".ts":   true,
	".rmvb": true,
}

var SubtitleExtensions = map[string]bool{
	".srt": true,
	".ass": true,
	".ssa": true,
	".sub": true,
	".vtt": true,
}

// IsVideo checks if a file name represents a video file
func IsVideo(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	return VideoExtensions[ext]
}

// IsSubtitle checks if a file name represents a subtitle file
func IsSubtitle(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	return SubtitleExtensions[ext]
}

// GetDirsSortedByDepth recursively finds all directories under root and sorts them by depth (shallowest first)
func GetDirsSortedByDepth(root string) ([]string, error) {
	var dirs []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(dirs, func(i, j int) bool {
		depthI := strings.Count(dirs[i], string(filepath.Separator))
		depthJ := strings.Count(dirs[j], string(filepath.Separator))
		if depthI == depthJ {
			return dirs[i] < dirs[j]
		}
		return depthI < depthJ
	})

	return dirs, nil
}

// FindFiles finds all video files or subtitle files recursively under root
func FindFiles(root string, matchFn func(string) bool) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && matchFn(info.Name()) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

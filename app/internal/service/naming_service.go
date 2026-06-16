package service

import (
	"fmt"
	"path/filepath"
	"regexp"
)

type NamingService interface {
	GetMoviePath(movieTitle string, year int, resolution string, ext string) (string, string)
	GetTVPath(tvTitle string, year int, season int, episode int, epTitle string, ext string) (string, string)
	GetTVBlurayPath(tvTitle string, year int, season int, srcDirName string) string
}

type namingService struct{}

func NewNamingService() NamingService {
	return &namingService{}
}

// GetMoviePath returns (destSubdir, destFilename)
// e.g. ("Inception (2010)", "Inception (2010) [1080p].mkv")
func (s *namingService) GetMoviePath(title string, year int, resolution string, ext string) (string, string) {
	var dirName string
	if year > 0 {
		dirName = fmt.Sprintf("%s (%d)", title, year)
	} else {
		dirName = title
	}

	var fileName string
	if resolution != "" {
		if year > 0 {
			fileName = fmt.Sprintf("%s (%d) [%s]%s", title, year, resolution, ext)
		} else {
			fileName = fmt.Sprintf("%s [%s]%s", title, resolution, ext)
		}
	} else {
		if year > 0 {
			fileName = fmt.Sprintf("%s (%d)%s", title, year, ext)
		} else {
			fileName = fmt.Sprintf("%s%s", title, ext)
		}
	}

	return dirName, fileName
}

// GetTVPath returns (destSubdir, destFilename)
// e.g. ("Game of Thrones (2011)/Season 01", "Game of Thrones (2011) - S01E05 - The Wolf and the Lion.mkv")
func (s *namingService) GetTVPath(title string, year int, season int, episode int, epTitle string, ext string) (string, string) {
	var tvDir string
	if year > 0 {
		tvDir = fmt.Sprintf("%s (%d)", title, year)
	} else {
		tvDir = title
	}

	seasonDir := fmt.Sprintf("Season %02d", season)
	subDir := filepath.Join(tvDir, seasonDir)

	var fileName string
	epPart := fmt.Sprintf("S%02dE%02d", season, episode)
	
	if epTitle != "" {
		if year > 0 {
			fileName = fmt.Sprintf("%s (%d) - %s - %s%s", title, year, epPart, epTitle, ext)
		} else {
			fileName = fmt.Sprintf("%s - %s - %s%s", title, epPart, epTitle, ext)
		}
	} else {
		if year > 0 {
			fileName = fmt.Sprintf("%s (%d) - %s%s", title, year, epPart, ext)
		} else {
			fileName = fmt.Sprintf("%s - %s%s", title, epPart, ext)
		}
	}

	return subDir, fileName
}

// GetTVBlurayPath returns target folder for a TV Blu-ray Disc
// e.g. "Game of Thrones (2011)/Season 01/Disc 01"
func (s *namingService) GetTVBlurayPath(title string, year int, season int, srcDirName string) string {
	var tvDir string
	if year > 0 {
		tvDir = fmt.Sprintf("%s (%d)", title, year)
	} else {
		tvDir = title
	}

	seasonDir := fmt.Sprintf("Season %02d", season)

	// Extract Disc folder name from original folder (e.g. "Disc 1", "D2", "DISC03")
	discReg := regexp.MustCompile(`(?i)\b(disc|d|disque)\s*(\d+)\b`)
	discName := "Disc 01"
	if matches := discReg.FindStringSubmatch(srcDirName); matches != nil {
		discNum := 1
		fmt.Sscanf(matches[2], "%d", &discNum)
		discName = fmt.Sprintf("Disc %02d", discNum)
	}

	return filepath.Join(tvDir, seasonDir, discName)
}

package parser

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Metadata struct {
	Title      string   `json:"title"`
	Year       int      `json:"year"`
	Season     int      `json:"season"`      // 0 if not TV
	Episodes   []int    `json:"episodes"`    // empty if Movie
	Resolution string   `json:"resolution"`  // 1080p, 2160p, 4k, etc.
	Source     string   `json:"source"`      // WEB-DL, BluRay, etc.
	Codec      string   `json:"codec"`       // h264, h265, hevc, etc.
	IsMovie    bool     `json:"is_movie"`
}

// Compilation of standard regex patterns
var (
	// TV Season/Episode patterns: S01E02-E04, S01E02, s01e02, etc.
	tvMultiEpisodeReg = regexp.MustCompile(`(?i)[sS](\d+)[eE](\d+)-?[eE](\d+)`)
	tvSingleEpisodeReg = regexp.MustCompile(`(?i)[sS](\d+)[eE](\d+)`)
	tvAlternativeReg   = regexp.MustCompile(`(?i)season\s*(\d+)\s*episode\s*(\d+)`)

	// Year pattern: 19xx or 20xx
	yearReg = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)

	// Resolution patterns
	resolutionReg = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|4k|8k|480p)\b`)

	// Source patterns
	sourceReg = regexp.MustCompile(`(?i)\b(web-dl|webrip|bluray|bdrip|hdtv|dvdrip)\b`)

	// Codec patterns
	codecReg = regexp.MustCompile(`(?i)\b(h264|h265|x264|x265|hevc|avc)\b`)
)

// ParseFilename extracts metadata from a file path or file name
func ParseFilename(path string) *Metadata {
	filename := filepath.Base(path)
	// Remove extension
	ext := filepath.Ext(filename)
	cleanName := strings.TrimSuffix(filename, ext)

	meta := &Metadata{
		IsMovie: true,
	}

	// 1. Check if TV and extract Season & Episodes
	if loc := tvMultiEpisodeReg.FindStringSubmatchIndex(cleanName); loc != nil {
		matches := tvMultiEpisodeReg.FindStringSubmatch(cleanName)
		season, _ := strconv.Atoi(matches[1])
		epStart, _ := strconv.Atoi(matches[2])
		epEnd, _ := strconv.Atoi(matches[3])
		
		meta.Season = season
		for i := epStart; i <= epEnd; i++ {
			meta.Episodes = append(meta.Episodes, i)
		}
		meta.IsMovie = false
		// Strip TV info from cleanName to help title cleaning
		cleanName = cleanName[:loc[0]] + " " + cleanName[loc[1]:]
	} else if loc := tvSingleEpisodeReg.FindStringSubmatchIndex(cleanName); loc != nil {
		matches := tvSingleEpisodeReg.FindStringSubmatch(cleanName)
		season, _ := strconv.Atoi(matches[1])
		episode, _ := strconv.Atoi(matches[2])
		
		meta.Season = season
		meta.Episodes = []int{episode}
		meta.IsMovie = false
		cleanName = cleanName[:loc[0]] + " " + cleanName[loc[1]:]
	} else if loc := tvAlternativeReg.FindStringSubmatchIndex(cleanName); loc != nil {
		matches := tvAlternativeReg.FindStringSubmatch(cleanName)
		season, _ := strconv.Atoi(matches[1])
		episode, _ := strconv.Atoi(matches[2])
		
		meta.Season = season
		meta.Episodes = []int{episode}
		meta.IsMovie = false
		cleanName = cleanName[:loc[0]] + " " + cleanName[loc[1]:]
	}

	// 2. Extract Year
	if loc := yearReg.FindStringSubmatchIndex(cleanName); loc != nil {
		matches := yearReg.FindStringSubmatch(cleanName)
		year, _ := strconv.Atoi(matches[1])
		meta.Year = year
		// Strip year
		cleanName = cleanName[:loc[0]] + " " + cleanName[loc[1]:]
	}

	// 3. Extract Resolution
	if loc := resolutionReg.FindStringSubmatchIndex(cleanName); loc != nil {
		matches := resolutionReg.FindStringSubmatch(cleanName)
		meta.Resolution = strings.ToLower(matches[1])
		cleanName = cleanName[:loc[0]] + " " + cleanName[loc[1]:]
	}

	// 4. Extract Source
	if loc := sourceReg.FindStringSubmatchIndex(cleanName); loc != nil {
		matches := sourceReg.FindStringSubmatch(cleanName)
		meta.Source = normalizeSource(matches[1])
		cleanName = cleanName[:loc[0]] + " " + cleanName[loc[1]:]
	}

	// 5. Extract Codec
	if loc := codecReg.FindStringSubmatchIndex(cleanName); loc != nil {
		matches := codecReg.FindStringSubmatch(cleanName)
		meta.Codec = strings.ToLower(matches[1])
		cleanName = cleanName[:loc[0]] + " " + cleanName[loc[1]:]
	}

	// 6. Clean Title
	// Replace dots, underscores, dashes with spaces
	titleClean := strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(cleanName)
	// Remove brackets and parenthesises content that might be left
	bracketsReg := regexp.MustCompile(`\[.*?\]|\(.*?\)`)
	titleClean = bracketsReg.ReplaceAllString(titleClean, " ")
	
	// Collapse multiple spaces
	spaceReg := regexp.MustCompile(`\s+`)
	titleClean = spaceReg.ReplaceAllString(titleClean, " ")
	
	// Trim spaces and extra symbols
	titleClean = strings.Trim(titleClean, " []()-+.")
	meta.Title = titleClean

	return meta
}

func normalizeSource(src string) string {
	srcUpper := strings.ToUpper(src)
	switch srcUpper {
	case "WEB-DL", "WEBRIP":
		return "WEB-DL"
	case "BLURAY", "BDRIP":
		return "BluRay"
	case "HDTV":
		return "HDTV"
	case "DVDRIP":
		return "DVDRip"
	default:
		return src
	}
}

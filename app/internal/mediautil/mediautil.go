// Package mediautil holds pure media/subtitle helpers shared between the
// MediaController (REST) and the SubtitleAgent service (MCP) so the disk-scan,
// NFO-reading and grouping logic is implemented once.
package mediautil

import (
	"context"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/storage"
	"github.com/bujic-movie/bujic-movie/pkg/fileutil"
	"github.com/bujic-movie/bujic-movie/pkg/mediainfo"
	"github.com/bujic-movie/bujic-movie/pkg/parser"
)

// SubtitleInfo describes one subtitle attached to a video file: an external
// file on disk or an internal (muxed) track.
type SubtitleInfo struct {
	Type     string `json:"type"` // "external" or "internal"
	Name     string `json:"name"` // filename for external, codec name for internal
	Language string `json:"language"`
	Title    string `json:"title"`
	Format   string `json:"format"` // srt, ass, pgs, etc.
	Path     string `json:"path"`   // absolute path for external, empty for internal
	Index    int    `json:"index"`
}

// GetSubtitlesForVideo lists external subtitle files next to the video and, on
// top, probes the video container for internal subtitle tracks (2s timeout).
func GetSubtitlesForVideo(videoPath string, stg storage.Storage) []SubtitleInfo {
	subs := make([]SubtitleInfo, 0)

	dir := filepath.Dir(videoPath)
	videoBase := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	items, err := stg.List(dir)
	if err == nil {
		for _, item := range items {
			if !item.IsDir && fileutil.IsSubtitle(item.Name) {
				if strings.HasPrefix(item.Name, videoBase) {
					subInfo := parser.ParseSubtitle(item.Path)
					subs = append(subs, SubtitleInfo{
						Type:     "external",
						Name:     item.Name,
						Language: subInfo.Language,
						Title:    "",
						Format:   subInfo.Format,
						Path:     item.Path,
						Index:    0,
					})
				}
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	details, err := mediainfo.Probe(ctx, videoPath)
	if err == nil && details != nil {
		for _, subStream := range details.Subtitle {
			lang := subStream.Language
			if lang == "" {
				lang = "unknown"
			}
			subs = append(subs, SubtitleInfo{
				Type:     "internal",
				Name:     subStream.Codec,
				Language: lang,
				Title:    subStream.Title,
				Format:   subStream.Micodec,
				Index:    subStream.Index,
			})
		}
	}

	return subs
}

// ExternalSubtitlesForVideo returns only the external (on-disk) subtitle files
// for a video, without probing the container. Cheap enough for list endpoints.
func ExternalSubtitlesForVideo(videoPath string, stg storage.Storage) []SubtitleInfo {
	subs := make([]SubtitleInfo, 0)
	dir := filepath.Dir(videoPath)
	videoBase := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	items, err := stg.List(dir)
	if err == nil {
		for _, item := range items {
			if !item.IsDir && fileutil.IsSubtitle(item.Name) {
				if strings.HasPrefix(item.Name, videoBase) {
					subInfo := parser.ParseSubtitle(item.Path)
					subs = append(subs, SubtitleInfo{
						Type:     "external",
						Name:     item.Name,
						Language: subInfo.Language,
						Format:   subInfo.Format,
						Path:     item.Path,
					})
				}
			}
		}
	}
	return subs
}

// SanitizeFilenameSuffix strips characters unsafe inside a quoted
// Content-Disposition filename or that could break headers.
func SanitizeFilenameSuffix(s string) string {
	replacer := strings.NewReplacer(
		`"`, "_",
		`/`, "_",
		`\`, "_",
		"\r", "_",
		"\n", "_",
		"\t", "_",
	)
	return replacer.Replace(s)
}

// CardAllow checks whether path is inside any media card root (ArchivePath and,
// when includeDownload is true, DownloadPath). Uses filepath.Clean and a
// separator prefix to prevent traversal (mirrors the REST allowlist, BR-09).
func CardAllow(path string, cards []entity.MediaCard, includeDownload bool) bool {
	clean := filepath.Clean(path)
	for _, card := range cards {
		for _, root := range []string{card.ArchivePath, card.DownloadPath} {
			if root == "" || (!includeDownload && root == card.DownloadPath) {
				continue
			}
			cleanRoot := filepath.Clean(root)
			if clean == cleanRoot {
				return true
			}
			if strings.HasPrefix(clean, cleanRoot+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

var (
	seasonDirRe = regexp.MustCompile(`(?i)^(?:season\s*|s)(\d+)$`)
	reTitle     = regexp.MustCompile(`(?i)<title>([^<]+)</title>`)
	reTMDB      = regexp.MustCompile(`(?i)<tmdbid>(\d+)</tmdbid>`)
	reUnique    = regexp.MustCompile(`(?i)<uniqueid[^>]*type="tmdb"[^>]*>(\d+)</uniqueid>`)
)

// NormalizeSeason derives the season from the file path/name for TV rows where
// the season is unknown. Mutates m in place.
func NormalizeSeason(m *entity.Media) {
	if m.Type == "tv" && m.Season == 0 {
		meta := parser.ParseFilename(m.Path)
		if meta.Season > 0 {
			m.Season = meta.Season
		} else {
			m.Season = 1
		}
	}
}

func getShowInfoFromPath(path string) (string, string) {
	parentDir := filepath.Clean(filepath.Dir(path))
	parentName := filepath.Base(parentDir)

	isSeasonDir := seasonDirRe.MatchString(parentName)

	var seriesDir string
	if isSeasonDir {
		seriesDir = filepath.Dir(parentDir)
	} else {
		seriesDir = parentDir
	}

	return seriesDir, filepath.Base(seriesDir)
}

func getShowTitleAndID(seriesDir string) (string, int) {
	nfoPath := filepath.Join(seriesDir, "tvshow.nfo")
	if _, err := os.Stat(nfoPath); err == nil {
		if data, err := os.ReadFile(nfoPath); err == nil {
			title := ""
			tmdbID := 0
			if m := reTitle.FindStringSubmatch(string(data)); len(m) > 1 {
				title = html.UnescapeString(m[1])
			}
			if m := reTMDB.FindStringSubmatch(string(data)); len(m) > 1 {
				tmdbID, _ = strconv.Atoi(m[1])
			}
			if tmdbID == 0 {
				if m := reUnique.FindStringSubmatch(string(data)); len(m) > 1 {
					tmdbID, _ = strconv.Atoi(m[1])
				}
			}
			if title == "" {
				title = filepath.Base(seriesDir)
			}
			return title, tmdbID
		}
	}
	return filepath.Base(seriesDir), 0
}

// GroupMedias flattens raw Media rows into the same "card" representation the
// REST /media list returns: movies grouped by TMDBID, TV rows grouped per
// series directory + season (title becomes "<Show> (第 N 季)", path becomes the
// season directory). Normalize runs before grouping (used for season backfill;
// may persist to the DB when wired by the caller).
func GroupMedias(rawMedias []entity.Media, normalize func(m *entity.Media)) []entity.Media {
	var grouped []entity.Media
	seen := make(map[string]int)

	for _, m := range rawMedias {
		if normalize != nil {
			normalize(&m)
		}

		if m.Type == "tv" {
			seriesDir, seriesName := getShowInfoFromPath(m.Path)
			showTitle, showTMDBID := getShowTitleAndID(seriesDir)
			if showTitle == "" {
				showTitle = seriesName
			}
			key := "tv-" + seriesDir + "-" + strconv.Itoa(m.Season)
			if _, ok := seen[key]; ok {
				continue
			}
			m.Title = showTitle + " (第 " + strconv.Itoa(m.Season) + " 季)"
			m.TMDBID = showTMDBID
			m.Path = filepath.Dir(m.Path)
			grouped = append(grouped, m)
			seen[key] = len(grouped) - 1
		} else {
			var key string
			if m.TMDBID > 0 {
				key = "movie-" + strconv.Itoa(m.TMDBID)
			} else {
				key = "movie-unmatched-" + strconv.Itoa(int(m.ID))
			}
			if _, ok := seen[key]; ok {
				continue
			}
			grouped = append(grouped, m)
			seen[key] = len(grouped) - 1
		}
	}
	return grouped
}

// ShowIDFromNFO is a small exported convenience returning show title + TMDB id
// for a video path (used to resolve episodes' parent info when needed).
func ShowIDFromNFO(videoPath string) (string, int) {
	seriesDir, _ := getShowInfoFromPath(videoPath)
	return getShowTitleAndID(seriesDir)
}

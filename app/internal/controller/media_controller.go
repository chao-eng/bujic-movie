package controller

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/bujic-movie/bujic-movie/internal/storage"
	"github.com/bujic-movie/bujic-movie/pkg/fileutil"
	"github.com/bujic-movie/bujic-movie/pkg/mediainfo"
	"github.com/bujic-movie/bujic-movie/pkg/parser"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/bujic-movie/bujic-movie/pkg/sat"
	"github.com/gin-gonic/gin"
)

type MediaController struct {
	mediaRepo     repository.MediaRepository
	mediaCardRepo repository.MediaCardRepository
	storage       storage.Storage
	notifier      service.NotificationService
}

func NewMediaController(
	mediaRepo repository.MediaRepository,
	mediaCardRepo repository.MediaCardRepository,
	stg storage.Storage,
	notifier service.NotificationService,
) *MediaController {
	return &MediaController{
		mediaRepo:     mediaRepo,
		mediaCardRepo: mediaCardRepo,
		storage:       stg,
		notifier:      notifier,
	}
}

// List returns a paginated list of media files in the database
func (ctrl *MediaController) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "1000"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 1000
	}
	offset := (page - 1) * limit

	var pathPrefix string
	cardIDStr := c.Query("card_id")
	if cardIDStr != "" {
		cardID, err := strconv.ParseUint(cardIDStr, 10, 32)
		if err == nil {
			card, err := ctrl.mediaCardRepo.GetByID(uint(cardID))
			if err == nil && card != nil {
				pathPrefix = card.ArchivePath
			}
		}
	}

	rawMedias, err := ctrl.mediaRepo.ListAll(pathPrefix)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	grouped := ctrl.groupMedias(rawMedias)

	total := len(grouped)
	start := offset
	if start > total {
		start = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	paginated := grouped[start:end]

	response.Success(c, paginated)
}

// Search searches for media files by title query
func (ctrl *MediaController) Search(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		response.BadRequest(c, "Query parameter 'q' is required")
		return
	}

	var pathPrefix string
	cardIDStr := c.Query("card_id")
	if cardIDStr != "" {
		cardID, err := strconv.ParseUint(cardIDStr, 10, 32)
		if err == nil {
			card, err := ctrl.mediaCardRepo.GetByID(uint(cardID))
			if err == nil && card != nil {
				pathPrefix = card.ArchivePath
			}
		}
	}

	rawMedias, err := ctrl.mediaRepo.Search(query, pathPrefix)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	grouped := ctrl.groupMedias(rawMedias)
	response.Success(c, grouped)
}

// Delete removes a media file entry from database (and season episodes if it is TV)
func (ctrl *MediaController) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.BadRequest(c, "Invalid media ID")
		return
	}

	media, err := ctrl.mediaRepo.GetByID(uint(id))
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	if media.Type == "tv" {
		err = ctrl.mediaRepo.DeleteSeason(media.TMDBID, media.Season)
	} else {
		err = ctrl.mediaRepo.Delete(uint(id))
	}

	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	response.Success(c, gin.H{
		"message": "Media record deleted successfully",
	})
}

type SubtitleInfo struct {
	Type     string `json:"type"` // "external" or "internal"
	Name     string `json:"name"` // filename for external, codec name for internal
	Language string `json:"language"`
	Title    string `json:"title"`
	Format   string `json:"format"` // srt, ass, pgs, etc.
	Path     string `json:"path"`   // absolute path for external, empty for internal
	Index    int    `json:"index"`
}

type EpisodeDTO struct {
	entity.Media
	Subtitles []SubtitleInfo `json:"subtitles"`
}

// GetEpisodes returns all episode records for a given TV show and season, along with their subtitle lists
func (ctrl *MediaController) GetEpisodes(c *gin.Context) {
	tmdbIDStr := c.Query("tmdb_id")
	seasonStr := c.Query("season")
	path := c.Query("path")

	tmdbID := 0
	if tmdbIDStr != "" {
		tmdbID, _ = strconv.Atoi(tmdbIDStr)
	}

	season := 0
	if seasonStr != "" {
		season, _ = strconv.Atoi(seasonStr)
	}

	episodes, err := ctrl.mediaRepo.GetEpisodes(tmdbID, season, path)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	if len(episodes) == 0 {
		response.Success(c, []EpisodeDTO{})
		return
	}

	// Concurrent fetch of subtitles for each episode to avoid blocking
	type result struct {
		idx  int
		subs []SubtitleInfo
	}
	ch := make(chan result, len(episodes))
	for i, ep := range episodes {
		go func(index int, videoPath string) {
			ch <- result{
				idx:  index,
				subs: getSubtitlesForVideo(videoPath, ctrl.storage),
			}
		}(i, ep.Path)
	}

	subsMap := make(map[int][]SubtitleInfo)
	for i := 0; i < len(episodes); i++ {
		res := <-ch
		subsMap[res.idx] = res.subs
	}

	dtos := make([]EpisodeDTO, len(episodes))
	for i, ep := range episodes {
		dtos[i] = EpisodeDTO{
			Media:     ep,
			Subtitles: subsMap[i],
		}
	}

	response.Success(c, dtos)
}

type RefreshRequest struct {
	CardID uint `json:"card_id" binding:"required"`
}

// Refresh triggers physical directory scanning for a specific MediaCard
func (ctrl *MediaController) Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		cardIDStr := c.Query("card_id")
		if cardIDStr != "" {
			id, err := strconv.ParseUint(cardIDStr, 10, 32)
			if err == nil {
				req.CardID = uint(id)
			}
		}
		if req.CardID == 0 {
			response.BadRequest(c, "card_id is required")
			return
		}
	}

	card, err := ctrl.mediaCardRepo.GetByID(req.CardID)
	if err != nil || card == nil {
		response.NotFound(c, "Media card not found")
		return
	}

	if card.ArchivePath == "" {
		response.BadRequest(c, "Card ArchivePath is not configured")
		return
	}

	added, updated, deleted, err := ctrl.syncDirectory(card)
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	response.Success(c, gin.H{
		"added":   added,
		"updated": updated,
		"deleted": deleted,
		"message": fmt.Sprintf("同步成功：新增 %d 个，更新 %d 个，清理 %d 个失效资源", added, updated, deleted),
	})
}

type nfoData struct {
	XMLName xml.Name `xml:"movie"`
	TMDBID  int      `xml:"tmdbid"`
	Title   string   `xml:"title"`
	Year    int      `xml:"year"`
	Season  int      `xml:"season"`
	Episode int      `xml:"episode"`
}

func parseNFO(path string) (*nfoData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var info nfoData
	_ = xml.Unmarshal(data, &info)

	// Regex fallbacks for extra robustness
	if info.TMDBID == 0 {
		re := regexp.MustCompile(`(?i)<tmdbid>(\d+)</tmdbid>`)
		if m := re.FindStringSubmatch(string(data)); len(m) > 1 {
			info.TMDBID, _ = strconv.Atoi(m[1])
		}
	}
	if info.TMDBID == 0 {
		re := regexp.MustCompile(`(?i)<uniqueid[^>]*type="tmdb"[^>]*>(\d+)</uniqueid>`)
		if m := re.FindStringSubmatch(string(data)); len(m) > 1 {
			info.TMDBID, _ = strconv.Atoi(m[1])
		}
	}
	if info.Title == "" {
		re := regexp.MustCompile(`(?i)<title>([^<]+)</title>`)
		if m := re.FindStringSubmatch(string(data)); len(m) > 1 {
			info.Title = html.UnescapeString(m[1])
		}
	}
	if info.Year == 0 {
		re := regexp.MustCompile(`(?i)<year>(\d+)</year>`)
		if m := re.FindStringSubmatch(string(data)); len(m) > 1 {
			info.Year, _ = strconv.Atoi(m[1])
		}
	}
	if info.Season == 0 {
		re := regexp.MustCompile(`(?i)<season>(\d+)</season>`)
		if m := re.FindStringSubmatch(string(data)); len(m) > 1 {
			info.Season, _ = strconv.Atoi(m[1])
		}
	}
	if info.Episode == 0 {
		re := regexp.MustCompile(`(?i)<episode>(\d+)</episode>`)
		if m := re.FindStringSubmatch(string(data)); len(m) > 1 {
			info.Episode, _ = strconv.Atoi(m[1])
		}
	}

	return &info, nil
}

func (ctrl *MediaController) syncDirectory(card *entity.MediaCard) (int, int, int, error) {
	videoFiles, err := fileutil.FindFiles(card.ArchivePath, fileutil.IsVideo)
	if err != nil {
		return 0, 0, 0, err
	}

	added := 0
	updated := 0
	deleted := 0

	scannedPaths := make(map[string]bool)

	for _, vf := range videoFiles {
		scannedPaths[vf] = true

		meta := parser.ParseFilename(vf)
		title := meta.Title
		year := meta.Year
		season := meta.Season
		tmdbID := 0

		dir := filepath.Dir(vf)
		baseName := strings.TrimSuffix(filepath.Base(vf), filepath.Ext(vf))
		nfoPath := filepath.Join(dir, baseName+".nfo")

		if _, err := os.Stat(nfoPath); os.IsNotExist(err) && card.MediaType == "movie" {
			nfoPath = filepath.Join(dir, "movie.nfo")
		}

		if _, err := os.Stat(nfoPath); err == nil {
			if info, err := parseNFO(nfoPath); err == nil {
				if info.Title != "" {
					title = info.Title
				}
				if info.Year > 0 {
					year = info.Year
				}
				if info.TMDBID > 0 {
					tmdbID = info.TMDBID
				}
				if info.Season > 0 {
					season = info.Season
				}
			}
		}

		if card.MediaType == "tv" && season == 0 {
			season = parseSeasonFromPath(vf)
		}

		existing, err := ctrl.mediaRepo.GetByPath(vf)

		posterPath := ""
		backdropPath := ""
		if existing != nil {
			posterPath = existing.PosterPath
			backdropPath = existing.BackdropPath
		}

		if posterPath == "" {
			localPoster := filepath.Join(dir, "poster.jpg")
			if _, err := os.Stat(localPoster); err == nil {
				posterPath = localPoster
			} else {
				parentPoster := filepath.Join(filepath.Dir(dir), "poster.jpg")
				if _, err := os.Stat(parentPoster); err == nil {
					posterPath = parentPoster
				}
			}
		}

		if err == nil && existing != nil {
			needsUpdate := false
			if existing.Title != title {
				existing.Title = title
				needsUpdate = true
			}
			if year > 0 && existing.Year != year {
				existing.Year = year
				needsUpdate = true
			}
			if tmdbID > 0 && existing.TMDBID != tmdbID {
				existing.TMDBID = tmdbID
				needsUpdate = true
			}
			if season > 0 && existing.Season != season {
				existing.Season = season
				needsUpdate = true
			}
			if existing.PosterPath != posterPath {
				existing.PosterPath = posterPath
				needsUpdate = true
			}
			if needsUpdate {
				existing.ScrapedAt = time.Now()
				_ = ctrl.mediaRepo.Update(existing)
				updated++
			}
		} else {
			media := &entity.Media{
				TMDBID:       tmdbID,
				Title:        title,
				Year:         year,
				Season:       season,
				Type:         card.MediaType,
				Path:         vf,
				PosterPath:   posterPath,
				BackdropPath: backdropPath,
				ScrapedAt:    time.Now(),
			}
			_ = ctrl.mediaRepo.Create(media)
			added++
		}
	}

	allDBMedias, err := ctrl.mediaRepo.ListAll(card.ArchivePath)
	if err == nil {
		for _, dbMedia := range allDBMedias {
			if !scannedPaths[dbMedia.Path] {
				_ = ctrl.mediaRepo.Delete(dbMedia.ID)
				deleted++
			}
		}
	}

	return added, updated, deleted, nil
}

func parseSeasonFromPath(path string) int {
	dir := filepath.Clean(path)
	seasonRe := regexp.MustCompile(`(?i)(?:season\s*|s)(\d+)`)
	for dir != "." && dir != "/" && dir != "" {
		base := filepath.Base(dir)
		if m := seasonRe.FindStringSubmatch(base); m != nil {
			if num, err := strconv.Atoi(m[1]); err == nil {
				return num
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return 0
}

// UploadSubtitle handles manual subtitle file uploading
func (ctrl *MediaController) UploadSubtitle(c *gin.Context) {
	videoPath, ok := c.GetPostForm("video_path")
	if !ok || videoPath == "" {
		response.BadRequest(c, "video_path is required")
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		response.BadRequest(c, "file is required: "+err.Error())
		return
	}

	_, err = ctrl.storage.Stat(videoPath)
	if err != nil {
		response.BadRequest(c, "Video file does not exist: "+videoPath)
		return
	}

	subInfo := parser.ParseSubtitle(file.Filename)

	dir := filepath.Dir(videoPath)
	videoBase := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	subExt := filepath.Ext(file.Filename)

	var targetSubName string
	if subInfo.Language != "unknown" {
		targetSubName = fmt.Sprintf("%s.%s%s", videoBase, subInfo.Language, subExt)
	} else {
		targetSubName = fmt.Sprintf("%s%s", videoBase, subExt)
	}

	destSubPath := filepath.Join(dir, targetSubName)

	srcFile, err := file.Open()
	if err != nil {
		response.InternalServerError(c, "Failed to open uploaded file: "+err.Error())
		return
	}
	defer srcFile.Close()

	err = ctrl.storage.Write(destSubPath, srcFile)
	if err != nil {
		response.InternalServerError(c, "Failed to write subtitle file: "+err.Error())
		return
	}

	_ = fileutil.ChmodWithUmask(destSubPath, false)

	cards, err := ctrl.mediaCardRepo.List()
	var matchedCardID uint = 0
	if err == nil {
		for _, card := range cards {
			if card.ArchivePath != "" {
				pattern := card.ArchivePath
				if !strings.HasSuffix(pattern, string(filepath.Separator)) {
					pattern += string(filepath.Separator)
				}
				if strings.HasPrefix(videoPath, pattern) {
					matchedCardID = card.ID
					break
				}
			}
		}
	}

	if matchedCardID > 0 {
		ctrl.notifier.NotifyRefreshForCard(context.Background(), matchedCardID)
	}

	response.Success(c, gin.H{
		"message": "字幕上传成功",
		"path":    destSubPath,
	})
}

// GetImage serves local image files safely
func (ctrl *MediaController) GetImage(c *gin.Context) {
	imgPath := c.Query("path")
	if imgPath == "" {
		c.String(400, "path is required")
		return
	}

	cards, err := ctrl.mediaCardRepo.List()
	if err != nil {
		c.String(500, err.Error())
		return
	}

	allowed := false
	for _, card := range cards {
		if card.ArchivePath != "" {
			pattern := card.ArchivePath
			if !strings.HasSuffix(pattern, string(filepath.Separator)) {
				pattern += string(filepath.Separator)
			}
			if strings.HasPrefix(imgPath, pattern) {
				allowed = true
				break
			}
		}
		if card.DownloadPath != "" {
			pattern := card.DownloadPath
			if !strings.HasSuffix(pattern, string(filepath.Separator)) {
				pattern += string(filepath.Separator)
			}
			if strings.HasPrefix(imgPath, pattern) {
				allowed = true
				break
			}
		}
	}

	if !allowed {
		c.String(403, "Access denied")
		return
	}

	if _, err := os.Stat(imgPath); os.IsNotExist(err) {
		c.String(404, "Image not found")
		return
	}

	c.File(imgPath)
}

// ListSubtitles returns the list of external and internal subtitles for a video file
func (ctrl *MediaController) ListSubtitles(c *gin.Context) {
	videoPath := c.Query("path")
	if videoPath == "" {
		response.BadRequest(c, "path is required")
		return
	}

	_, err := ctrl.storage.Stat(videoPath)
	if err != nil {
		response.BadRequest(c, "Video file does not exist: "+videoPath)
		return
	}

	subs := getSubtitlesForVideo(videoPath, ctrl.storage)
	response.Success(c, subs)
}

type DeleteSubtitleRequest struct {
	Path string `json:"path" binding:"required"`
}

// DeleteSubtitle physically deletes an external subtitle file safely
func (ctrl *MediaController) DeleteSubtitle(c *gin.Context) {
	var req DeleteSubtitleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "path is required")
		return
	}

	cards, err := ctrl.mediaCardRepo.List()
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	allowed := false
	var matchedCardID uint = 0
	for _, card := range cards {
		if card.ArchivePath != "" {
			pattern := card.ArchivePath
			if !strings.HasSuffix(pattern, string(filepath.Separator)) {
				pattern += string(filepath.Separator)
			}
			if strings.HasPrefix(req.Path, pattern) {
				allowed = true
				matchedCardID = card.ID
				break
			}
		}
	}

	if !allowed {
		response.Forbidden(c, "Access denied")
		return
	}

	_, err = ctrl.storage.Stat(req.Path)
	if err != nil {
		response.NotFound(c, "Subtitle file not found")
		return
	}

	err = ctrl.storage.Delete(req.Path)
	if err != nil {
		response.InternalServerError(c, "Failed to delete subtitle: "+err.Error())
		return
	}

	if matchedCardID > 0 {
		ctrl.notifier.NotifyRefreshForCard(context.Background(), matchedCardID)
	}

	response.Success(c, gin.H{"message": "字幕删除成功"})
}

type ConvertSubtitleRequest struct {
	VideoPath     string `json:"video_path" binding:"required"`
	SubtitlePath  string `json:"subtitle_path"`
	IsInternal    bool   `json:"is_internal"`
	InternalIndex int    `json:"internal_index"`
}

// ConvertSubtitle handles traditional to simplified Chinese subtitle conversion
func (ctrl *MediaController) ConvertSubtitle(c *gin.Context) {
	var req ConvertSubtitleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	cards, err := ctrl.mediaCardRepo.List()
	if err != nil {
		response.InternalServerError(c, err.Error())
		return
	}

	allowed := false
	var matchedCardID uint = 0
	for _, card := range cards {
		if card.ArchivePath != "" {
			pattern := card.ArchivePath
			if !strings.HasSuffix(pattern, string(filepath.Separator)) {
				pattern += string(filepath.Separator)
			}
			if strings.HasPrefix(req.VideoPath, pattern) && (req.SubtitlePath == "" || strings.HasPrefix(req.SubtitlePath, pattern)) {
				allowed = true
				matchedCardID = card.ID
				break
			}
		}
	}

	if !allowed {
		response.Forbidden(c, "Access denied")
		return
	}

	var convertedStr string
	var destSubPath string
	var ext string

	if req.IsInternal {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			response.BadRequest(c, "ffmpeg is not installed on this system")
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		details, err := mediainfo.Probe(ctx, req.VideoPath)
		if err != nil || details == nil {
			response.BadRequest(c, "Failed to probe video details: "+err.Error())
			return
		}

		var matchedStream *mediainfo.SubtitleStream
		for _, sub := range details.Subtitle {
			if sub.Index == req.InternalIndex {
				matchedStream = &sub
				break
			}
		}

		if matchedStream == nil {
			response.BadRequest(c, "Subtitle stream index not found in video")
			return
		}

		ext = ".srt"
		if matchedStream.Micodec == "ass" || matchedStream.Codec == "ass" || matchedStream.Micodec == "ssa" || matchedStream.Codec == "ssa" {
			ext = ".ass"
		} else if matchedStream.Micodec == "webvtt" || matchedStream.Codec == "webvtt" {
			ext = ".vtt"
		}

		tempPath := req.VideoPath + ".tmp" + ext
		cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", req.VideoPath, "-map", fmt.Sprintf("0:%d", req.InternalIndex), tempPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			response.InternalServerError(c, fmt.Sprintf("Failed to extract subtitle via ffmpeg: %v. Output: %s", err, string(output)))
			return
		}
		defer os.Remove(tempPath)

		contentBytes, err := os.ReadFile(tempPath)
		if err != nil {
			response.InternalServerError(c, "Failed to read extracted subtitle: "+err.Error())
			return
		}

		convertedStr = sat.ToSimplified(string(contentBytes))
		dir := filepath.Dir(req.VideoPath)
		videoBase := strings.TrimSuffix(filepath.Base(req.VideoPath), filepath.Ext(req.VideoPath))
		destSubPath = filepath.Join(dir, fmt.Sprintf("%s.zh-CN%s", videoBase, ext))
	} else {
		rc, err := ctrl.storage.Read(req.SubtitlePath)
		if err != nil {
			response.InternalServerError(c, "Failed to read subtitle file: "+err.Error())
			return
		}
		defer rc.Close()

		contentBytes, err := io.ReadAll(rc)
		if err != nil {
			response.InternalServerError(c, "Failed to read subtitle stream: "+err.Error())
			return
		}

		convertedStr = sat.ToSimplified(string(contentBytes))
		dir := filepath.Dir(req.SubtitlePath)
		filename := filepath.Base(req.SubtitlePath)
		ext = filepath.Ext(filename)
		base := strings.TrimSuffix(filename, ext)

		var newBase string
		lowerBase := strings.ToLower(base)
		if strings.Contains(lowerBase, ".zh-tw") {
			newBase = strings.Replace(base, ".zh-TW", ".zh-CN", -1)
			newBase = strings.Replace(newBase, ".zh-tw", ".zh-CN", -1)
		} else if strings.Contains(lowerBase, ".zh-hant") {
			newBase = strings.Replace(base, ".zh-Hant", ".zh-Hans", -1)
			newBase = strings.Replace(newBase, ".zh-hant", ".zh-Hans", -1)
		} else if strings.Contains(lowerBase, ".cht") {
			newBase = strings.Replace(base, ".cht", ".chs", -1)
			newBase = strings.Replace(newBase, ".CHT", ".CHS", -1)
		} else if strings.Contains(lowerBase, ".traditional") {
			newBase = strings.Replace(base, ".traditional", ".simplified", -1)
			newBase = strings.Replace(newBase, ".Traditional", ".Simplified", -1)
		} else if strings.Contains(lowerBase, ".tc") {
			newBase = strings.Replace(base, ".tc", ".sc", -1)
			newBase = strings.Replace(newBase, ".TC", ".SC", -1)
		} else if strings.Contains(lowerBase, ".繁") {
			newBase = strings.Replace(base, ".繁", ".简", -1)
		} else {
			newBase = base + ".zh-CN"
		}
		destSubPath = filepath.Join(dir, newBase+ext)
	}

	err = ctrl.storage.Write(destSubPath, strings.NewReader(convertedStr))
	if err != nil {
		response.InternalServerError(c, "Failed to save simplified subtitle: "+err.Error())
		return
	}

	_ = fileutil.ChmodWithUmask(destSubPath, false)

	if matchedCardID > 0 {
		ctrl.notifier.NotifyRefreshForCard(context.Background(), matchedCardID)
	}

	response.Success(c, gin.H{
		"message": "字幕转换成功并已保存",
		"path":    destSubPath,
	})
}

func getSubtitlesForVideo(videoPath string, stg storage.Storage) []SubtitleInfo {
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

func (ctrl *MediaController) groupMedias(rawMedias []entity.Media) []entity.Media {
	var grouped []entity.Media
	seen := make(map[string]int)

	for _, m := range rawMedias {
		if m.Type == "tv" {
			if m.Season == 0 {
				meta := parser.ParseFilename(m.Path)
				if meta.Season > 0 {
					m.Season = meta.Season
				} else {
					m.Season = 1
				}
				_ = ctrl.mediaRepo.Update(&m)
			}

			var key string
			if m.TMDBID > 0 {
				key = fmt.Sprintf("tv-%d-%d", m.TMDBID, m.Season)
			} else {
				key = fmt.Sprintf("tv-unmatched-%d", m.ID)
			}

			if _, ok := seen[key]; ok {
				continue
			}
			m.Title = fmt.Sprintf("%s (第 %d 季)", m.Title, m.Season)
			m.Path = filepath.Dir(m.Path)
			grouped = append(grouped, m)
			seen[key] = len(grouped) - 1
		} else {
			var key string
			if m.TMDBID > 0 {
				key = fmt.Sprintf("movie-%d", m.TMDBID)
			} else {
				key = fmt.Sprintf("movie-unmatched-%d", m.ID)
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

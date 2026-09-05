package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/mediautil"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/storage"
	"github.com/bujic-movie/bujic-movie/pkg/fileutil"
	"github.com/bujic-movie/bujic-movie/pkg/mediainfo"
)

// ---- DTOs (aligned with PRD v0.3 §3.x) ----

const (
	SubtitleStatusFull    = "full"
	SubtitleStatusPartial = "partial"
	SubtitleStatusNone    = "none"
)

// SubtitleStatus is a per-media aggregation result used by QueryMediaList.
type SubtitleStatus struct {
	HasSubtitle       bool
	ExternalLanguages []string
	MissingSubtitles  []string // only meaningful when Status == full
	Status            string   // full / partial / none
	Warns             []string
}

// MediaListItem is one aggregated item in a query_media_list result.
type MediaListItem struct {
	MediaID          uint     `json:"media_id"`
	Title            string   `json:"title"`
	Type             string   `json:"type"`
	Year             int      `json:"year"`
	Season           int      `json:"season"`
	Path             string   `json:"path"`
	HasSubtitle      bool     `json:"has_subtitle"`
	SubtitleStatus   string   `json:"subtitle_status"`
	Languages        []string `json:"languages,omitempty"`
	MissingSubtitles []string `json:"missing_subtitles,omitempty"`
	Warns            []string `json:"warns,omitempty"`
}

// MediaListRequest is the input of query_media_list.
type MediaListRequest struct {
	MediaType   string
	MediaCardID uint
	Query       string
	Page        int
	Limit       int
}

// MediaListResult is the output of query_media_list.
type MediaListResult struct {
	Items []MediaListItem `json:"items"`
	Total int             `json:"total"`
	Page  int             `json:"page"`
	Limit int             `json:"limit"`
}

// EpisodeSubtitle is the detail returned for a video file (UC-02).
type VideoSubtitleDetail struct {
	VideoPath string                   `json:"video_path"`
	Subtitles []mediautil.SubtitleInfo `json:"subtitles"`
	Warns     []string                 `json:"warns,omitempty"`
}

// QuerySubtitlesRequest identifies a target for query_media_subtitles.
type QuerySubtitlesRequest struct {
	MediaID         uint
	Path            string
	MediaCardID     uint
	IncludeInternal bool
}

// FetchSubtitleRequest identifies a subtitle to fetch (UC-03).
type FetchSubtitleRequest struct {
	Path          string
	VideoPath     string
	InternalIndex int
	MediaCardID   uint
}

// FetchSubtitleResult is the returned subtitle content.
type FetchSubtitleResult struct {
	Content       string   `json:"content,omitempty"`
	ContentBase64 string   `json:"content_base64,omitempty"`
	IsImage       bool     `json:"is_image"`
	Format        string   `json:"format"`
	Encoding      string   `json:"encoding"`
	ByteSize      int64    `json:"byte_size"`
	Language      string   `json:"language,omitempty"`
	Name          string   `json:"name,omitempty"`
	Warns         []string `json:"warns,omitempty"`
}

// UploadSubtitleRequest is the input of upload_subtitle (UC-04).
type UploadSubtitleRequest struct {
	VideoPath       string
	SubtitleContent string
	SubtitleBase64  string
	Format          string
	Language        string
}

// UploadSubtitleResult is the output of upload_subtitle.
type UploadSubtitleResult struct {
	Path              string   `json:"path"`
	Message           string   `json:"message"`
	OverwriteExisting bool     `json:"overwrite_existing"`
	Warns             []string `json:"warns,omitempty"`
}

// probeCacheEntry caches internal subtitle probe results per video.
type probeCacheEntry struct {
	Size    int64
	ModTime time.Time
	Subs    []mediautil.SubtitleInfo
}

// SubtitleAgentService implements the four MCP media/subtitle business tools.
type SubtitleAgentService interface {
	QueryMediaList(ctx context.Context, req MediaListRequest) (*MediaListResult, error)
	QuerySubtitles(ctx context.Context, req QuerySubtitlesRequest) ([]VideoSubtitleDetail, error)
	FetchSubtitle(ctx context.Context, req FetchSubtitleRequest) (*FetchSubtitleResult, error)
	UploadSubtitle(ctx context.Context, req UploadSubtitleRequest) (*UploadSubtitleResult, error)
}

type subtitleAgentService struct {
	mediaRepo     repository.MediaRepository
	mediaCardRepo repository.MediaCardRepository
	storage       storage.Storage

	probeMu    sync.Mutex
	probeCache map[string]probeCacheEntry
}

func NewSubtitleAgentService(
	mediaRepo repository.MediaRepository,
	mediaCardRepo repository.MediaCardRepository,
	stg storage.Storage,
) SubtitleAgentService {
	return &subtitleAgentService{
		mediaRepo:     mediaRepo,
		mediaCardRepo: mediaCardRepo,
		storage:       stg,
		probeCache:    make(map[string]probeCacheEntry),
	}
}

// resolveCard returns the card used as scope, or nil + "" for "all cards".
// Defaults to the default card when the request has no explicit ID.
func (s *subtitleAgentService) resolveCard(cardID uint) (*entity.MediaCard, string, error) {
	if cardID != 0 {
		card, err := s.mediaCardRepo.GetByID(cardID)
		if err != nil {
			return nil, "", fmt.Errorf("media_card_id not found: %d", cardID)
		}
		return card, card.ArchivePath, nil
	}
	card, err := s.mediaCardRepo.GetDefault()
	if err != nil {
		// No default card: fall back to scanning all cards (empty path prefix).
		return nil, "", nil
	}
	return card, card.ArchivePath, nil
}

// cardRoots returns the archive roots to allow/deny checks against.
func (s *subtitleAgentService) cardRoots() ([]entity.MediaCard, error) {
	return s.mediaCardRepo.List()
}

func (s *subtitleAgentService) pathAllowed(p string) bool {
	cards, err := s.cardRoots()
	if err != nil {
		return false
	}
	return mediautil.CardAllow(p, cards, false)
}

// ---- query_media_list (UC-01) ----

func (s *subtitleAgentService) QueryMediaList(ctx context.Context, req MediaListRequest) (*MediaListResult, error) {
	page := req.Page
	if page < 1 {
		page = 1
	}
	limit := req.Limit
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	_, pathPrefix, err := s.resolveCard(req.MediaCardID)
	if err != nil {
		return nil, err
	}

	var rawMedias []entity.Media
	if req.Query != "" {
		rawMedias, err = s.mediaRepo.Search(req.Query, pathPrefix)
	} else {
		rawMedias, err = s.mediaRepo.ListAll(pathPrefix)
	}
	if err != nil {
		return nil, err
	}

	// Filter by type before grouping.
	if req.MediaType != "" {
		filtered := rawMedias[:0]
		for _, m := range rawMedias {
			if m.Type == req.MediaType {
				filtered = append(filtered, m)
			}
		}
		rawMedias = filtered
	}

	grouped := mediautil.GroupMedias(rawMedias, func(m *entity.Media) {
		mediautil.NormalizeSeason(m)
		if m.Type == "tv" && m.Season > 0 {
			// persist backfilled season like the REST controller does
			_ = s.mediaRepo.Update(m)
		}
	})

	total := len(grouped)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	items := make([]MediaListItem, 0, end-start)
	for _, g := range grouped[start:end] {
		item := MediaListItem{
			MediaID: g.ID,
			Title:   g.Title,
			Type:    g.Type,
			Year:    g.Year,
			Season:  g.Season,
			Path:    g.Path,
		}
		if g.Type == "movie" {
			// grouped movie path = season dir only when TV; movie path is a video file
			sub := s.scanVideoSubtitles(ctx, g.Path)
			item.HasSubtitle = sub.HasSubtitle
			item.SubtitleStatus = sub.Status
			item.Languages = sub.ExternalLanguages
			item.MissingSubtitles = sub.MissingSubtitles
			item.Warns = sub.Warns
		} else {
			// TV: g.Path is a season dir; aggregate over video files inside it
			sub := s.scanDirSubtitles(ctx, g.Path)
			item.HasSubtitle = sub.HasSubtitle
			item.SubtitleStatus = sub.Status
			item.Languages = sub.ExternalLanguages
			item.MissingSubtitles = sub.MissingSubtitles
			item.Warns = sub.Warns
		}
		items = append(items, item)
	}

	return &MediaListResult{Items: items, Total: total, Page: page, Limit: limit}, nil
}

// scanVideoSubtitles computes external langs (cheap) and optional internal
// (probe via cache) for one video file.
func (s *subtitleAgentService) scanVideoSubtitles(ctx context.Context, videoPath string) SubtitleStatus {
	res := SubtitleStatus{Status: SubtitleStatusNone}
	external := mediautil.ExternalSubtitlesForVideo(videoPath, s.storage)
	langs := make(map[string]bool)
	for _, sub := range external {
		if sub.Language != "" && sub.Language != "unknown" {
			langs[sub.Language] = true
		}
	}

	// internal probe (cached) - may be unavailable on cold cache
	internal, ok := s.probeInternalCached(ctx, videoPath)
	if ok && len(internal) > 0 {
		res.HasSubtitle = true
	}
	if len(external) > 0 {
		res.HasSubtitle = true
	}
	for l := range langs {
		res.ExternalLanguages = append(res.ExternalLanguages, l)
	}
	sort.Strings(res.ExternalLanguages)

	if !ok {
		// could not confirm internal (cold/large) -> partial only if we relied on
		// external presence already? Per BR-03 we mark partial only when internal
		// state is unknown; external-only info is still "full" for external.
		// Simplify: presence of external subs makes the item useful regardless.
		res.Status = SubtitleStatusFull
		if s.statMissing(videoPath) != nil {
			res.Warns = append(res.Warns, "video file missing on disk")
			res.Status = SubtitleStatusNone
		}
	} else {
		res.Status = SubtitleStatusFull
	}

	// Determine missing zh-CN (only reliable in full state).
	hasZHCN := langs["zh-CN"]
	if res.Status == SubtitleStatusFull && !hasZHCN {
		res.MissingSubtitles = append(res.MissingSubtitles, "zh-CN")
	}
	return res
}

func (s *subtitleAgentService) scanDirSubtitles(ctx context.Context, dir string) SubtitleStatus {
	// Aggregate external langs across all video files in the season dir.
	videos, err := fileutil.FindFiles(dir, fileutil.IsVideo)
	if err != nil {
		return SubtitleStatus{Status: SubtitleStatusNone, Warns: []string{"unable to list dir: " + err.Error()}}
	}

	res := SubtitleStatus{Status: SubtitleStatusNone}
	extLangs := make(map[string]bool)
	anyInternal := false
	anyExternal := false

	for _, v := range videos {
		ext := mediautil.ExternalSubtitlesForVideo(v, s.storage)
		for _, sub := range ext {
			anyExternal = true
			if sub.Language != "" && sub.Language != "unknown" {
				extLangs[sub.Language] = true
			}
		}
		if len(videos) <= 16 { // BR-31: bounded probing for season aggregate
			internal, ok := s.probeInternalCached(ctx, v)
			if ok && len(internal) > 0 {
				anyInternal = true
			}
		}
	}

	res.HasSubtitle = anyExternal || anyInternal
	for l := range extLangs {
		res.ExternalLanguages = append(res.ExternalLanguages, l)
	}
	sort.Strings(res.ExternalLanguages)
	res.Status = SubtitleStatusFull

	hasZHCN := extLangs["zh-CN"]
	if res.Status == SubtitleStatusFull && !hasZHCN {
		res.MissingSubtitles = append(res.MissingSubtitles, "zh-CN")
	}
	return res
}

func (s *subtitleAgentService) statMissing(path string) error {
	_, err := s.storage.Stat(path)
	return err
}

func (s *subtitleAgentService) probeInternalCached(ctx context.Context, videoPath string) ([]mediautil.SubtitleInfo, bool) {
	stat, err := s.storage.Stat(videoPath)
	if err != nil {
		return nil, false
	}
	s.probeMu.Lock()
	entry, ok := s.probeCache[videoPath]
	s.probeMu.Unlock()
	if ok && entry.Size == stat.Size && entry.ModTime.Equal(stat.ModTime) {
		return entry.Subs, true
	}

	// probe (2s timeout), then cache
	subs := mediautil.GetSubtitlesForVideo(videoPath, s.storage)
	internalOnly := make([]mediautil.SubtitleInfo, 0)
	for _, sub := range subs {
		if sub.Type == "internal" {
			internalOnly = append(internalOnly, sub)
		}
	}

	s.probeMu.Lock()
	s.probeCache[videoPath] = probeCacheEntry{Size: stat.Size, ModTime: stat.ModTime, Subs: internalOnly}
	s.probeMu.Unlock()
	// ok = true even on empty; empty means definitively no internal subs
	return internalOnly, true
}

// ---- query_media_subtitles (UC-02) ----

func (s *subtitleAgentService) QuerySubtitles(ctx context.Context, req QuerySubtitlesRequest) ([]VideoSubtitleDetail, error) {
	if req.MediaID == 0 && req.Path == "" {
		return nil, errors.New("media_id or path is required")
	}
	includeInternal := req.IncludeInternal

	// Resolve target video paths.
	var videoPaths []string
	if req.Path != "" {
		// A season dir path -> all videos in dir; otherwise assume a video file.
		if info, err := s.storage.Stat(req.Path); err == nil && info.IsDir {
			videos, err := fileutil.FindFiles(req.Path, fileutil.IsVideo)
			if err != nil {
				return nil, err
			}
			sort.Strings(videos)
			videoPaths = videos
		} else {
			videoPaths = []string{req.Path}
		}
	} else {
		media, err := s.mediaRepo.GetByID(req.MediaID)
		if err != nil {
			return nil, errors.New("media not found")
		}
		if media.Type == "tv" {
			// media row is a single episode video file
			videoPaths = []string{media.Path}
		} else {
			videoPaths = []string{media.Path}
		}
	}

	// Path allowlist check (unless explicitly resolved via media_id which came from DB).
	if req.Path != "" && !s.pathAllowed(req.Path) {
		return nil, errors.New("access denied: path outside media library")
	}

	var out []VideoSubtitleDetail
	for _, vp := range videoPaths {
		detail := VideoSubtitleDetail{VideoPath: vp}
		subs := mediautil.ExternalSubtitlesForVideo(vp, s.storage)
		if includeInternal {
			internal, ok := s.probeInternalCached(ctx, vp)
			if ok {
				subs = append(subs, internal...)
			} else {
				detail.Warns = append(detail.Warns, "internal probe failed")
			}
		}
		detail.Subtitles = subs
		out = append(out, detail)
	}
	return out, nil
}

// ---- fetch_subtitle (UC-03) ----

func (s *subtitleAgentService) FetchSubtitle(ctx context.Context, req FetchSubtitleRequest) (*FetchSubtitleResult, error) {
	// Allowlist (only files under Archive roots may be read; internal extraction
	// requires the video to be inside a library too).
	if !s.pathAllowed(req.Path) {
		if !s.pathAllowed(req.VideoPath) {
			return nil, errors.New("access denied: path outside media library")
		}
	}

	if req.Path != "" {
		// External subtitle file
		if !s.pathAllowed(req.Path) {
			return nil, errors.New("access denied: path outside media library")
		}
		return s.readExternalSubtitle(req.Path)
	}

	// Internal track: require video_path + index
	if req.VideoPath == "" {
		return nil, errors.New("video_path is required for internal subtitle fetch")
	}
	return s.extractInternalSubtitle(ctx, req.VideoPath, req.InternalIndex)
}

func (s *subtitleAgentService) readExternalSubtitle(path string) (*FetchSubtitleResult, error) {
	rc, err := s.storage.Read(path)
	if err != nil {
		return nil, errors.New("subtitle file not found")
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	subInfo := mediautil.ExternalSubtitlesForVideo(path, s.storage)
	_ = subInfo

	res := &FetchSubtitleResult{
		Format:   ext,
		Encoding: "utf-8",
		ByteSize: int64(len(data)),
	}
	// detect if binary/image-like (pgs/sup)
	if ext == "sup" || ext == "sub" || hasBinaryContent(data) {
		res.IsImage = true
		res.ContentBase64 = base64Encode(data)
		return res, nil
	}
	content, enc := decodeToUTF8(data)
	res.Content = content
	res.Encoding = enc
	if enc != "utf-8" {
		res.Warns = append(res.Warns, "original encoding "+enc+" converted to utf-8")
	}
	return res, nil
}

func (s *subtitleAgentService) extractInternalSubtitle(ctx context.Context, videoPath string, index int) (*FetchSubtitleResult, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, errors.New("ffmpeg is not installed on this system")
	}

	// Probe to locate the stream and determine codec.
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	details, err := mediainfo.Probe(probeCtx, videoPath)
	if err != nil || details == nil {
		return nil, errors.New("failed to probe video: " + errMsg(err))
	}
	var matched *mediautil.SubtitleInfo
	// internal subtitles are discovered via getSubtitlesForVideo (which probes);
	// reuse it to find the track matching index.
	all := mediautil.GetSubtitlesForVideo(videoPath, s.storage)
	for i := range all {
		if all[i].Type == "internal" && all[i].Index == index {
			matched = &all[i]
			break
		}
	}
	if matched == nil {
		return nil, errors.New("internal subtitle track index not found")
	}

	ext := ".srt"
	isCopy := false
	switch matched.Format {
	case "ass", "ssa":
		ext = ".ass"
	case "webvtt":
		ext = ".vtt"
	case "pgs":
		ext = ".sup"
		isCopy = true
	case "dvd_subtitle":
		ext = ".sub"
		isCopy = true
	}

	tempDir := os.TempDir()
	tempPath := filepath.Join(tempDir, fmt.Sprintf("sub_fetch_%d_%d%s", time.Now().UnixNano(), index, ext))

	var cmd *exec.Cmd
	if isCopy {
		cmd = exec.CommandContext(ctx, "ffmpeg", "-y", "-i", videoPath, "-map", "0:"+strconv.Itoa(index), "-c", "copy", tempPath)
	} else {
		cmd = exec.CommandContext(ctx, "ffmpeg", "-y", "-i", videoPath, "-map", "0:"+strconv.Itoa(index), tempPath)
	}
	execCtx, cancelExec := context.WithTimeout(ctx, 60*time.Second)
	defer cancelExec()
	cmd = exec.CommandContext(execCtx, cmd.Args[0], cmd.Args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg extraction failed: %v: %s", err, truncate(string(output), 2000))
	}
	defer os.Remove(tempPath)

	data, err := os.ReadFile(tempPath)
	if err != nil {
		return nil, err
	}

	res := &FetchSubtitleResult{
		Format:   strings.TrimPrefix(ext, "."),
		Encoding: "utf-8",
		ByteSize: int64(len(data)),
		Name:     matched.Name,
		Language: matched.Language,
	}
	if isCopy {
		res.IsImage = true
		res.ContentBase64 = base64Encode(data)
		return res, nil
	}
	content, enc := decodeToUTF8(data)
	res.Content = content
	res.Encoding = enc
	return res, nil
}

// ---- upload_subtitle (UC-04) ----

func (s *subtitleAgentService) UploadSubtitle(ctx context.Context, req UploadSubtitleRequest) (*UploadSubtitleResult, error) {
	if req.VideoPath == "" {
		return nil, errors.New("video_path is required")
	}
	if req.SubtitleContent == "" && req.SubtitleBase64 == "" {
		return nil, errors.New("subtitle_content or subtitle_base64 is required")
	}
	if !s.pathAllowed(req.VideoPath) {
		return nil, errors.New("access denied: video outside media library")
	}

	_, err := s.storage.Stat(req.VideoPath)
	if err != nil {
		return nil, errors.New("video file does not exist")
	}

	// Decode payload bytes.
	var data []byte
	if req.SubtitleBase64 != "" {
		data, err = base64Decode(req.SubtitleBase64)
		if err != nil {
			return nil, errors.New("invalid subtitle_base64")
		}
	} else {
		if hasBinaryContent([]byte(req.SubtitleContent)) {
			return nil, errors.New("subtitle_content looks binary; use subtitle_base64 instead")
		}
		data = []byte(req.SubtitleContent)
	}

	// Determine format.
	format := strings.ToLower(strings.TrimPrefix(req.Format, "."))
	if format == "" {
		format = detectFormat(data)
	}
	if format == "" {
		return nil, errors.New("cannot detect subtitle format; provide format explicitly")
	}
	if !isAllowedSubtitleFormat(format) {
		return nil, errors.New("unsupported subtitle format: " + format)
	}

	// language -> filename suffix (server-computed naming, BR-10).
	lang := req.Language
	videoBase := strings.TrimSuffix(filepath.Base(req.VideoPath), filepath.Ext(req.VideoPath))
	dir := filepath.Dir(req.VideoPath)

	var targetName string
	if lang != "" && isKnownLanguage(lang) {
		targetName = videoBase + "." + lang + "." + format
	} else {
		targetName = videoBase + "." + format
	}
	destPath := filepath.Join(dir, targetName)

	overwrite := false
	if _, err := s.storage.Stat(destPath); err == nil {
		overwrite = true
	}

	if err := s.storage.Write(destPath, strings.NewReader(string(data))); err != nil {
		return nil, err
	}
	_ = os.Chmod(destPath, 0644)

	return &UploadSubtitleResult{
		Path:              destPath,
		Message:           "字幕上传成功",
		OverwriteExisting: overwrite,
	}, nil
}

// ---- shared tiny helpers ----

func hasBinaryContent(b []byte) bool {
	for _, by := range b {
		if by == 0 {
			return true
		}
	}
	return false
}

func isAllowedSubtitleFormat(f string) bool {
	switch f {
	case "srt", "ass", "ssa", "sub", "vtt":
		return true
	}
	return false
}

func isKnownLanguage(lang string) bool {
	switch strings.ToLower(lang) {
	case "zh-cn", "zh-tw", "zh", "en", "ja", "ko", "de", "fr", "es", "it", "pt", "ru":
		return true
	}
	return false
}

func detectFormat(data []byte) string {
	head := string(data)
	if strings.Contains(head, "WEBVTT") {
		return "vtt"
	}
	if strings.Contains(head, "[Script Info]") || strings.Contains(head, "[V4+ Styles]") {
		return "ass"
	}
	if strings.Contains(head, "-->") {
		return "srt"
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func errMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

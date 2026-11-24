package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/service/downloadclient"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
)

// GrabRequest is the API payload for pushing one result to a download client.
type GrabRequest struct {
	Fingerprint      string `json:"fingerprint"`
	IndexerID        uint   `json:"indexer_id"`
	DownloadURL      string `json:"download_url"`
	Title            string `json:"title"`
	SizeBytes        int64  `json:"size_bytes"`
	InfoHash         string `json:"info_hash"`
	DownloadType     string `json:"download_type"`
	CategoryOverride string `json:"category_override"`
	Categories       []int  `json:"categories"`
	ClientID         uint   `json:"client_id"`
}

// GrabPreview answers "which downloader and category would this grab use?"
// before the user confirms. The UI needs it to pre-fill the category instead of
// asking the user to guess (BR-19).
type GrabPreview struct {
	ClientID     uint     `json:"client_id"`
	ClientName   string   `json:"client_name"`
	ClientKind   string   `json:"client_kind"`
	Category     string   `json:"category"`
	CategoryFrom string   `json:"category_from"` // mapping / parent / default / none
	Categories   []string `json:"categories"`
	SavePath     string   `json:"save_path"`
	SavePathHint string   `json:"save_path_hint"`
	// Capabilities lets the dialog hide the category picker for a client that
	// has no categories at all.
	Capabilities downloadclient.Capabilities `json:"capabilities"`
	Warning      string                      `json:"warning,omitempty"`
}

// GrabResult reports what happened to a grab request.
type GrabResult struct {
	RecordID   uint   `json:"record_id"`
	ClientID   uint   `json:"client_id"`
	ClientName string `json:"client_name"`
	Category   string `json:"category"`
	SavePath   string `json:"save_path"`
	InfoHash   string `json:"info_hash"`
	Status     string `json:"status"`
	Duplicate  bool   `json:"duplicate"`
	Message    string `json:"message,omitempty"`
}

// GrabError kinds mirror the PRD error codes.
const (
	GrabKindDuplicate = "duplicate"
	GrabKindClient    = "client"
	GrabKindCategory  = "category"
	GrabKindPending   = "pending"
)

// GrabService turns a search result into a download task and records it.
type GrabService struct {
	repo      repository.SearchRepository
	indexers  *IndexerService
	clients   *DownloadClientService
	cfg       *config.Config
	metrics   *SearchMetrics
	mu        sync.Mutex
	lastPrune time.Time
}

// NewGrabService wires the service.
func NewGrabService(repo repository.SearchRepository, indexers *IndexerService, clients *DownloadClientService, cfg *config.Config, metrics *SearchMetrics) *GrabService {
	return &GrabService{repo: repo, indexers: indexers, clients: clients, cfg: cfg, metrics: metrics}
}

func (s *GrabService) window() time.Duration {
	if s.cfg != nil && s.cfg.Search.GrabWindowMS > 0 {
		return time.Duration(s.cfg.Search.GrabWindowMS) * time.Millisecond
	}
	return 60 * time.Second
}

func (s *GrabService) timeout() time.Duration {
	if s.cfg != nil && s.cfg.Search.GrabTimeoutMS > 0 {
		return time.Duration(s.cfg.Search.GrabTimeoutMS) * time.Millisecond
	}
	return 15 * time.Second
}

func (s *GrabService) retention() time.Duration {
	days := 90
	if s.cfg != nil && s.cfg.Search.RecordRetentionDays > 0 {
		days = s.cfg.Search.RecordRetentionDays
	}
	return time.Duration(days) * 24 * time.Hour
}

// Grab pushes one release. Idempotency is keyed on the fingerprint with a 60s
// window (BR-09); failures are recorded so the UI can explain them.
// Preview resolves the downloader and category for a release without pushing
// anything, so the confirmation dialog can show and allow editing the real
// choice (AC-21 / AC-35).
func (s *GrabService) Preview(ctx context.Context, req GrabRequest) (*GrabPreview, error) {
	if req.IndexerID == 0 {
		return nil, &InputError{Field: "indexer_id", Message: "缺少站点标识"}
	}
	indexer, err := s.repo.GetIndexer(req.IndexerID)
	if err != nil {
		return nil, &InputError{Field: "indexer_id", Message: "站点不存在"}
	}
	client, err := s.clients.Select(s.boundClientID(*indexer, req.ClientID))
	if err != nil {
		return nil, err
	}

	target, err := s.clients.ResolveTarget(client.ID, req.Categories)
	if err != nil {
		return nil, err
	}

	info, _ := downloadclient.LookupKind(client.Kind)
	preview := &GrabPreview{
		ClientID: client.ID, ClientName: client.Name, ClientKind: client.Kind,
		Category: target.Category, CategoryFrom: target.Source, SavePath: target.SavePath,
		Capabilities: info.Capabilities,
	}
	switch {
	case target.SavePath != "" && target.Category != "":
		// Both are sent: the category keeps its label, the path overrides the
		// folder the category would have chosen.
		preview.SavePathHint = target.SavePath + "（覆盖分类 " + target.Category + " 的目录）"
	case target.SavePath != "":
		preview.SavePathHint = target.SavePath
	case target.Source == "mapping" || target.Source == "parent":
		preview.SavePathHint = "由下载器的分类规则决定"
	case target.Source == "default":
		preview.SavePathHint = "由下载器的默认分类决定"
		preview.Warning = "这个资源的分类没有配置映射，将使用默认分类"
	default:
		preview.SavePathHint = "由下载器的默认下载目录决定"
		preview.Warning = "没有可用的分类或目录，将由下载器自己决定"
	}
	// Offered targets: everything the client already has, plus every mapped
	// target, so the user can pick something that is actually valid.
	options := []string{}
	if cats, err := s.clients.Categories(ctx, client.ID); err == nil {
		for _, c := range cats {
			options = append(options, c.Name)
		}
	}
	if mappings, err := s.clients.ListCategoryMappings(client.ID); err == nil {
		for _, m := range mappings {
			options = append(options, m.ClientCategory)
		}
	}
	preview.Categories = dedupeStrings(options)
	return preview, nil
}

func (s *GrabService) Grab(ctx context.Context, req GrabRequest) (*GrabResult, error) {
	if strings.TrimSpace(req.Fingerprint) == "" {
		return nil, &InputError{Field: "fingerprint", Message: "缺少结果指纹"}
	}
	if req.IndexerID == 0 {
		return nil, &InputError{Field: "indexer_id", Message: "缺少站点标识"}
	}
	indexer, err := s.repo.GetIndexer(req.IndexerID)
	if err != nil {
		return nil, &InputError{Field: "indexer_id", Message: "站点不存在"}
	}

	// Duplicate within the window returns the existing task (BR-09).
	if existing, err := s.repo.FindRecentRecord(req.Fingerprint, time.Now().Add(-s.window())); err == nil && existing.Status != "failed" {
		result := &GrabResult{
			RecordID: existing.ID, ClientID: existing.DownloaderID, ClientName: existing.DownloaderName,
			Category: existing.ClientCategory, SavePath: existing.SavePath, InfoHash: existing.InfoHash,
			Status: existing.Status, Duplicate: true, Message: "该结果已在下载器中",
		}
		s.metrics.RecordDuplicateGrab()
		return result, &GrabError{Kind: GrabKindDuplicate, Message: result.Message}
	}

	client, err := s.clients.Select(s.boundClientID(*indexer, req.ClientID))
	if err != nil {
		return nil, err
	}

	target, err := s.clients.ResolveTarget(client.ID, categoriesOf(req))
	if err != nil {
		return nil, err
	}
	category := strings.TrimSpace(req.CategoryOverride)
	if category != "" {
		// A client without categories has nothing to apply an override to; the
		// directory already came from the mapping.
		if info, _ := downloadclient.LookupKind(client.Kind); info.Capabilities.Categories {
			target.Category = category
		}
	}

	record := &entity.SearchDownloadRecord{
		Title:          strings.TrimSpace(req.Title),
		IndexerID:      indexer.ID,
		IndexerName:    indexer.Name,
		DownloaderID:   client.ID,
		DownloaderName: client.Name,
		SizeBytes:      req.SizeBytes,
		ClientCategory: target.Category,
		SavePath:       target.SavePath,
		Status:         "pending",
		InfoHash:       req.InfoHash,
		Fingerprint:    req.Fingerprint,
		CreatedAt:      time.Now(),
	}
	if record.Title == "" {
		record.Title = req.Fingerprint
	}

	payload := strings.TrimSpace(req.DownloadURL)
	if payload == "" {
		return nil, &InputError{Field: "download_url", Message: "缺少下载链接"}
	}

	grabCtx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	clientImpl := s.clients.Client(*client)
	pushErr := clientImpl.Push(grabCtx, downloadclient.PushOptions{
		Kind:          req.DownloadType,
		Payload:       payload,
		Name:          req.Title,
		Category:      target.Category,
		SavePath:      target.SavePath,
		Paused:        client.InitialState == "pause",
		Sequential:    client.Sequential,
		FirstLast:     client.FirstLastFirst,
		ContentLayout: client.ContentLayout,
	})

	switch {
	case pushErr == nil:
		record.Status = "created"
		s.clients.MarkAvailable(client.ID)
		s.indexers.RecordSuccess(indexer.ID, 0)
	case isDuplicateErr(pushErr):
		record.Status = "created"
		s.clients.MarkAvailable(client.ID)
	case errors.Is(pushErr, context.DeadlineExceeded) || isTimeoutErr(pushErr):
		// Result unknown: keep pending so the task list can reconcile.
		record.Status = "pending"
		record.ErrorKind = "timeout"
	default:
		record.Status = "failed"
		record.ErrorKind = errorKindOf(pushErr)
		s.clients.MarkUnavailable(client.ID)
	}

	if err := s.repo.CreateRecord(record); err != nil {
		return nil, &StorageError{Err: err}
	}
	s.pruneRecords()

	result := &GrabResult{
		RecordID: record.ID, ClientID: client.ID, ClientName: client.Name,
		Category: target.Category, SavePath: target.SavePath,
		InfoHash: record.InfoHash, Status: record.Status,
	}
	s.metrics.RecordEvent("grab_result", 0, record.Status == "created")
	switch record.Status {
	case "created":
		result.Message = "任务已创建"
		return result, nil
	case "pending":
		result.Message = "结果未知，请到下载任务页确认"
		return result, &GrabError{Kind: GrabKindPending, Message: result.Message}
	default:
		result.Message = failureText(pushErr)
		return result, &GrabError{Kind: GrabKindClient, Message: result.Message}
	}
}

// GrabError is a classified grab failure with an optional existing record.
type GrabError struct {
	Kind    string
	Message string
}

func (e *GrabError) Error() string { return e.Message }

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func (s *GrabService) boundClientID(indexer entity.SearchIndexer, requested uint) uint {
	if requested > 0 {
		return requested
	}
	return indexer.DownloadClientID
}

func categoriesOf(req GrabRequest) []int {
	return req.Categories
}

// isDuplicateErr reports "the client already has this torrent". A plain
// rejection stays a failure, so the user is not told a task was created when it
// was not.
func isDuplicateErr(err error) bool {
	var cErr *downloadclient.Error
	return downloadclient.As(err, &cErr) && cErr.Kind == downloadclient.KindDuplicate
}

func isTimeoutErr(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var cErr *downloadclient.Error
	return downloadclient.As(err, &cErr) && strings.Contains(cErr.Message, "超时")
}

func errorKindOf(err error) string {
	var cErr *downloadclient.Error
	if downloadclient.As(err, &cErr) {
		return cErr.Kind
	}
	return "unknown"
}

func failureText(err error) string {
	if err == nil {
		return ""
	}
	var cErr *downloadclient.Error
	if downloadclient.As(err, &cErr) {
		return cErr.Message
	}
	return "推送失败"
}

func (s *GrabService) pruneRecords() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastPrune) < 10*time.Minute {
		return
	}
	s.lastPrune = time.Now()
	if err := s.repo.DeleteRecordsBefore(time.Now().Add(-s.retention())); err != nil {
		logger.Warn("search: record prune failed: %v", err)
	}
}

// Records lists recent download records.
func (s *GrabService) Records(limit int) ([]entity.SearchDownloadRecord, error) {
	return s.repo.ListRecords(limit)
}

// TaskGroup is one client's task snapshot.
type TaskGroup struct {
	ClientID      uint                  `json:"client_id"`
	ClientName    string                `json:"client_name"`
	State         string                `json:"state"` // ok / unreachable
	DataUpdatedAt string                `json:"data_updated_at"`
	Tasks         []downloadclient.Task `json:"tasks"`
	Message       string                `json:"message,omitempty"`
}

// TaskService aggregates tasks from every enabled client (BR-37).
type TaskService struct {
	repo      repository.SearchRepository
	clients   *DownloadClientService
	mu        sync.Mutex
	snapshots map[uint]taskSnapshot
}

type taskSnapshot struct {
	tasks     []downloadclient.Task
	updatedAt time.Time
}

// NewTaskService wires the service.
func NewTaskService(repo repository.SearchRepository, clients *DownloadClientService) *TaskService {
	return &TaskService{repo: repo, clients: clients, snapshots: map[uint]taskSnapshot{}}
}

// Groups queries every enabled client concurrently and degrades per group.
func (s *TaskService) Groups(ctx context.Context) ([]TaskGroup, error) {
	clients, err := s.repo.EnabledClients()
	if err != nil {
		return nil, err
	}
	groups := make([]TaskGroup, len(clients))
	var wg sync.WaitGroup
	start := time.Now()
	for i, client := range clients {
		wg.Add(1)
		go func(i int, client entity.SearchDownloadClient) {
			defer wg.Done()
			group := TaskGroup{ClientID: client.ID, ClientName: client.Name}
			callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			tasks, err := s.clients.Client(client).Tasks(callCtx)
			if err != nil {
				group.State = "unreachable"
				group.Message = "下载器不可达"
				if snapshot, ok := s.snapshot(client.ID); ok {
					group.Tasks = snapshot.tasks
					group.DataUpdatedAt = formatTime(snapshot.updatedAt)
				}
				groups[i] = group
				return
			}
			group.State = "ok"
			group.Tasks = tasks
			group.DataUpdatedAt = formatTime(time.Now())
			s.saveSnapshot(client.ID, tasks)
			groups[i] = group
		}(i, client)
	}
	wg.Wait()
	_ = start
	return groups, nil
}

func (s *TaskService) snapshot(id uint) (taskSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.snapshots[id]
	return snap, ok
}

func (s *TaskService) saveSnapshot(id uint, tasks []downloadclient.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[id] = taskSnapshot{tasks: tasks, updatedAt: time.Now()}
}

// Action applies pause/resume/delete to one task.
func (s *TaskService) Action(ctx context.Context, clientID uint, hash, action string, deleteFiles bool) error {
	client, err := s.repo.GetClient(clientID)
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.clients.Client(*client).Action(callCtx, hash, action, deleteFiles); err != nil {
		var cErr *downloadclient.Error
		if downloadclient.As(err, &cErr) && (cErr.Kind == downloadclient.KindRejected || cErr.Kind == downloadclient.KindUnsupported) {
			return &InputError{Field: "action", Message: cErr.Message}
		}
		return &ClientUnavailable{Err: err}
	}
	if action == "delete" {
		s.saveSnapshot(clientID, nil)
	}
	return nil
}

// ClientUnavailable marks a degraded task group.
type ClientUnavailable struct{ Err error }

func (e *ClientUnavailable) Error() string { return "下载器不可达" }
func (e *ClientUnavailable) Unwrap() error { return e.Err }

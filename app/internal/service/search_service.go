package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/service/connector"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"gorm.io/gorm"
)

// SearchFailure is one indexer that did not contribute results.
type SearchFailure struct {
	IndexerID   uint   `json:"indexer_id"`
	IndexerName string `json:"indexer_name"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
}

// SearchItem is one merged result card.
type SearchItem struct {
	Fingerprint    string         `json:"fingerprint"`
	Title          string         `json:"title"`
	IndexerID      uint           `json:"indexer_id"`
	IndexerName    string         `json:"indexer_name"`
	Sources        []SearchSource `json:"sources"`
	Categories     []int          `json:"categories"`
	SizeBytes      int64          `json:"size_bytes"`
	PublishDate    string         `json:"publish_date"`
	Seeders        int            `json:"seeders"`
	Peers          int            `json:"peers"`
	DownloadType   string         `json:"download_type"`
	DownloadURL    string         `json:"download_url"`
	InfoHash       string         `json:"info_hash"`
	DetailsURL     string         `json:"details_url"`
	CategoryLabels []string       `json:"category_labels"`
}

// SearchSource is one indexer variant of a merged card.
type SearchSource struct {
	IndexerID   uint   `json:"indexer_id"`
	IndexerName string `json:"indexer_name"`
	DownloadURL string `json:"download_url"`
	InfoHash    string `json:"info_hash"`
	Seeders     int    `json:"seeders"`
	SizeBytes   int64  `json:"size_bytes"`
	DetailsURL  string `json:"details_url"`
}

// SearchResult is the response payload for one query.
type SearchResult struct {
	SearchID   string          `json:"search_id"`
	RequestID  string          `json:"request_id"`
	ElapsedMS  int64           `json:"elapsed_ms"`
	Total      int             `json:"total"`
	Truncated  bool            `json:"truncated"`
	Failures   []SearchFailure `json:"failures"`
	Items      []SearchItem    `json:"items"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	Skipped    int             `json:"skipped"`
	Irrelevant int             `json:"irrelevant"`
	TotalFound int             `json:"total_found"`
}

// SearchQuery is the normalised input for a search.
type SearchQuery struct {
	RequestID  string
	Keyword    string
	Categories []int
	IndexerIDs []uint
	Mode       string
	MinSeeders int
	MinSize    int64
	MaxSize    int64
	Page       int
	PageSize   int
	Sort       string
	Season     int
	Episode    int
	Year       int
	IMDBID     string
	TMDBID     string
}

// SearchService fans out a query across enabled indexers with global and
// per-site concurrency limits, then dedupes, filters, sorts and paginates.
type SearchService struct {
	repo      repository.SearchRepository
	indexers  *IndexerService
	cfg       *config.Config
	metrics   *SearchMetrics
	globalSem chan struct{}
	mu        sync.Mutex
	inFlight  map[string]struct{}
	siteMu    sync.Mutex
	siteSems  map[uint]chan struct{}
}

// MaxResultItems caps one response (BR-15).
const MaxResultItems = 500

// MaxPageSize caps one page.
const MaxPageSize = 50

// NewSearchService builds the service with the configured concurrency budget.
func NewSearchService(repo repository.SearchRepository, indexers *IndexerService, cfg *config.Config) *SearchService {
	limit := 3
	if cfg != nil && cfg.Search.MaxGlobalSearches > 0 {
		limit = cfg.Search.MaxGlobalSearches
	}
	return &SearchService{
		repo: repo, indexers: indexers, cfg: cfg, metrics: NewSearchMetrics(),
		globalSem: make(chan struct{}, limit),
		inFlight:  map[string]struct{}{},
		siteSems:  map[uint]chan struct{}{},
	}
}

func (s *SearchService) deadline() time.Duration {
	if s.cfg != nil && s.cfg.Search.SearchDeadlineMS > 0 {
		return time.Duration(s.cfg.Search.SearchDeadlineMS) * time.Millisecond
	}
	return 15 * time.Second
}

func (s *SearchService) siteTimeout() time.Duration {
	if s.cfg != nil && s.cfg.Search.SiteTimeoutMS > 0 {
		return time.Duration(s.cfg.Search.SiteTimeoutMS) * time.Millisecond
	}
	return 8 * time.Second
}

func (s *SearchService) maxWorkers() int {
	if s.cfg != nil && s.cfg.Search.MaxSiteWorkers > 0 {
		return s.cfg.Search.MaxSiteWorkers
	}
	return 20
}

func (s *SearchService) historyLimit() int {
	if s.cfg != nil && s.cfg.Search.HistoryLimit > 0 {
		return s.cfg.Search.HistoryLimit
	}
	return 20
}

// ErrNoIndexers means nothing is available to query.
var ErrNoIndexers = errors.New("no usable indexer")

// ErrBusy means another search with the same request id is running.
var ErrBusy = errors.New("search already in flight")

// Search runs one aggregated query.
func (s *SearchService) Search(ctx context.Context, q SearchQuery) (*SearchResult, error) {
	start := time.Now()

	if q.PageSize <= 0 || q.PageSize > MaxPageSize {
		q.PageSize = MaxPageSize
	}
	if q.Page <= 0 {
		q.Page = 1
	}

	// Global concurrency: queue for a slot, bounded by the search deadline.
	deadline := start.Add(s.deadline())
	waitCtx, cancelWait := context.WithDeadline(ctx, deadline)
	defer cancelWait()
	select {
	case s.globalSem <- struct{}{}:
		defer func() { <-s.globalSem }()
	case <-waitCtx.Done():
		return nil, fmt.Errorf("排队超时：同时进行的搜索过多")
	}

	if q.RequestID != "" {
		s.mu.Lock()
		if _, exists := s.inFlight[q.RequestID]; exists {
			s.mu.Unlock()
			return nil, ErrBusy
		}
		s.inFlight[q.RequestID] = struct{}{}
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.inFlight, q.RequestID)
			s.mu.Unlock()
		}()
	}

	indexers, err := s.candidates(q.IndexerIDs)
	if err != nil {
		s.metrics.RecordSearch(time.Since(start), true)
		return nil, err
	}
	if len(indexers) == 0 {
		s.metrics.RecordSearch(time.Since(start), true)
		return nil, ErrNoIndexers
	}

	searchCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	type outcome struct {
		indexer  entity.SearchIndexer
		releases []connector.Release
		err      error
		skipped  bool
	}

	results := make(chan outcome, len(indexers))
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.maxWorkers())

	queueStart := time.Now()
	for _, idx := range indexers {
		wg.Add(1)
		go func(idx entity.SearchIndexer) {
			defer wg.Done()
			// Worker pool: a site that cannot be scheduled within 8s is skipped.
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-time.After(s.siteTimeout()):
				results <- outcome{indexer: idx, skipped: true, err: &connector.Error{Kind: connector.KindTimeout, Message: "站点排队超时"}}
				return
			}
			if time.Since(queueStart) > s.siteTimeout() {
				results <- outcome{indexer: idx, skipped: true, err: &connector.Error{Kind: connector.KindTimeout, Message: "站点排队超时"}}
				return
			}

			release, ok := s.acquireSite(searchCtx, idx.ID)
			if !ok {
				results <- outcome{indexer: idx, skipped: true, err: &connector.Error{Kind: connector.KindTimeout, Message: "站点排队超时"}}
				return
			}
			defer release()

			conn, err := s.indexers.Connector(searchCtx, idx.ID)
			if err != nil {
				results <- outcome{indexer: idx, err: err}
				return
			}
			if aware, ok := conn.(interface{ SupportsCategories([]int) bool }); ok && !aware.SupportsCategories(q.Categories) {
				// AC-27: do not query a site that declares no support for the
				// requested category. It is not a failure, just not applicable.
				results <- outcome{indexer: idx, skipped: true}
				return
			}
			releases, err := conn.Search(searchCtx, connector.Query{
				Keyword: q.Keyword, Categories: q.Categories, Season: q.Season,
				Episode: q.Episode, Year: q.Year, IMDBID: q.IMDBID, TMDBID: q.TMDBID,
				Page: q.Page - 1, Limit: MaxPageSize, Mode: q.Mode,
			})
			for i := range releases {
				releases[i].IndexerID = idx.ID
				releases[i].IndexerName = idx.Name
			}
			results <- outcome{indexer: idx, releases: releases, err: err}
		}(idx)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var all []connector.Release
	failures := make([]SearchFailure, 0)
	skipped := 0
	irrelevant := 0
	for res := range results {
		if res.err == nil && res.skipped {
			skipped++
			continue
		}
		if res.err != nil {
			if res.skipped {
				skipped++
			}
			failures = append(failures, SearchFailure{
				IndexerID:   res.indexer.ID,
				IndexerName: res.indexer.Name,
				Kind:        failureKind(res.err),
				Message:     failureMessage(res.err),
			})
			s.indexers.RecordFailure(res.indexer.ID, failureKind(res.err))
			continue
		}
		s.indexers.RecordSuccess(res.indexer.ID, 0)
		// A site whose definition went stale may ignore the keyword and return
		// its default listing; dropping the batch keeps the merged list useful
		// (BUG-008). It is reported as a failure so the user can see why.
		kept, dropped := connector.DropIrrelevantBatch(res.releases, q.Keyword)
		if dropped {
			irrelevant++
			failures = append(failures, SearchFailure{
				IndexerID:   res.indexer.ID,
				IndexerName: res.indexer.Name,
				Kind:        "irrelevant",
				Message:     irrelevantReason(q.Keyword),
			})
			continue
		}
		all = append(all, kept...)
	}

	merged := connector.GroupReleases(all)
	filtered := s.applyFilters(merged, q)
	connector.SortReleases(filtered, q.Sort)

	totalFound := len(filtered)
	truncated := false
	if len(filtered) > MaxResultItems {
		// BR-15: cap the payload and report the truncation, but keep the count
		// consistent with what is actually reachable through paging.
		filtered = filtered[:MaxResultItems]
		truncated = true
		totalFound = MaxResultItems
	}

	// Pagination happens after sorting so the page is stable.
	startIdx := (q.Page - 1) * q.PageSize
	if startIdx > len(filtered) {
		startIdx = len(filtered)
	}
	endIdx := startIdx + q.PageSize
	if endIdx > len(filtered) {
		endIdx = len(filtered)
	}
	page := filtered[startIdx:endIdx]

	items := make([]SearchItem, 0, len(page))
	for _, card := range page {
		items = append(items, toSearchItem(card))
	}

	if q.Keyword != "" {
		if err := s.repo.TouchHistory(q.Keyword); err != nil {
			logger.Warn("search: history touch failed: %v", err)
		}
		s.pruneHistory()
	}

	sort.SliceStable(failures, func(i, j int) bool { return failures[i].IndexerID < failures[j].IndexerID })

	s.metrics.RecordSearch(time.Since(start), false)

	return &SearchResult{
		SearchID:   fmt.Sprintf("s_%d", start.UnixNano()),
		RequestID:  q.RequestID,
		ElapsedMS:  time.Since(start).Milliseconds(),
		Total:      len(items),
		TotalFound: totalFound,
		Truncated:  truncated,
		Failures:   failures,
		Items:      items,
		Page:       q.Page,
		PageSize:   q.PageSize,
		Skipped:    skipped,
		Irrelevant: irrelevant,
	}, nil
}

// acquireSite enforces one in-flight request per indexer (BR-29). Waiting is
// bounded by 15s or the remaining search budget, whichever is shorter.
func (s *SearchService) acquireSite(ctx context.Context, id uint) (func(), bool) {
	s.siteMu.Lock()
	sem, ok := s.siteSems[id]
	if !ok {
		sem = make(chan struct{}, 1)
		s.siteSems[id] = sem
	}
	s.siteMu.Unlock()

	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, true
	case <-waitCtx.Done():
		return nil, false
	}
}

func (s *SearchService) candidates(ids []uint) ([]entity.SearchIndexer, error) {
	enabled, err := s.repo.EnabledIndexers()
	if err != nil {
		return nil, err
	}
	wanted := map[uint]bool{}
	for _, id := range ids {
		wanted[id] = true
	}

	out := make([]entity.SearchIndexer, 0, len(enabled))
	now := time.Now()
	for _, idx := range enabled {
		if len(wanted) > 0 && !wanted[idx.ID] {
			continue
		}
		status, err := s.repo.GetStatus(idx.ID)
		if err == nil && status.CooldownUntil.After(now) {
			continue
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		out = append(out, idx)
	}
	return out, nil
}

func (s *SearchService) applyFilters(cards []connector.MergedRelease, q SearchQuery) []connector.MergedRelease {
	out := make([]connector.MergedRelease, 0, len(cards))
	for _, card := range cards {
		if q.MinSeeders > 0 && card.Seeders < q.MinSeeders {
			continue
		}
		if q.MinSize > 0 && card.SizeBytes > 0 && card.SizeBytes < q.MinSize {
			continue
		}
		if q.MaxSize > 0 && card.SizeBytes > q.MaxSize {
			continue
		}
		if len(q.Categories) > 0 && !categoryMatches(card.Categories, q.Categories) {
			continue
		}
		out = append(out, card)
	}
	return out
}

// categoryMatches treats a 4-digit group (2000) as covering its children (2040).
func categoryMatches(have, want []int) bool {
	for _, w := range want {
		for _, h := range have {
			if h == w {
				return true
			}
			if len(fmt.Sprint(h)) == 4 && len(fmt.Sprint(w)) == 4 && fmt.Sprint(h)[:2] == fmt.Sprint(w)[:2] {
				return true
			}
		}
	}
	return false
}

func toSearchItem(card connector.MergedRelease) SearchItem {
	sources := make([]SearchSource, 0, len(card.Sources))
	for _, src := range card.Sources {
		sources = append(sources, SearchSource{
			IndexerID:   src.IndexerID,
			IndexerName: src.IndexerName,
			DownloadURL: src.DownloadURL,
			InfoHash:    src.InfoHash,
			Seeders:     src.Seeders,
			SizeBytes:   src.SizeBytes,
			DetailsURL:  src.DetailsURL,
		})
	}
	return SearchItem{
		Fingerprint:    connector.Fingerprint(card.Title, card.SizeBytes),
		Title:          card.Title,
		IndexerID:      card.IndexerID,
		IndexerName:    card.IndexerName,
		Sources:        sources,
		Categories:     card.Categories,
		SizeBytes:      card.SizeBytes,
		PublishDate:    card.PublishDate,
		Seeders:        card.Seeders,
		Peers:          card.Peers,
		DownloadType:   card.DownloadType,
		DownloadURL:    card.DownloadURL,
		InfoHash:       card.InfoHash,
		DetailsURL:     card.DetailsURL,
		CategoryLabels: card.CategoryNames,
	}
}

// irrelevantReason explains why a site's batch was dropped. Sites that only
// index latin titles rewrite Chinese keywords to punctuation, so their "results"
// are just the front page.
func irrelevantReason(keyword string) string {
	for _, r := range keyword {
		if r > 127 {
			return "该站点只支持英文关键词，中文搜索无结果，已忽略本次返回"
		}
	}
	return "搜索结果与关键词无关，已忽略该站点本次结果"
}

func failureKind(err error) string {
	var cErr *connector.Error
	if errors.As(err, &cErr) {
		return cErr.Kind
	}
	return "unknown"
}

func failureMessage(err error) string {
	var cErr *connector.Error
	if errors.As(err, &cErr) {
		return cErr.Message
	}
	return "站点查询失败"
}

// Metrics exposes the in-process counters.
func (s *SearchService) Metrics() *SearchMetrics { return s.metrics }

// History returns recent keywords.
func (s *SearchService) History() ([]entity.SearchHistory, error) {
	return s.repo.ListHistory(s.historyLimit())
}

// DeleteHistory removes one keyword.
func (s *SearchService) DeleteHistory(keyword string) error {
	return s.repo.DeleteHistory(keyword)
}

// ClearHistory removes every keyword.
func (s *SearchService) ClearHistory() error {
	return s.repo.ClearHistory()
}

func (s *SearchService) pruneHistory() {
	list, err := s.repo.ListHistory(0)
	if err != nil {
		return
	}
	limit := s.historyLimit()
	if len(list) <= limit {
		return
	}
	for _, entry := range list[limit:] {
		_ = s.repo.DeleteHistory(entry.Keyword)
	}
}

// NormaliseKeyword trims and collapses whitespace in a user keyword.
func NormaliseKeyword(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}

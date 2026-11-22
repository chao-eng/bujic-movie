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
	Fingerprint string         `json:"fingerprint"`
	Title       string         `json:"title"`
	IndexerID   uint           `json:"indexer_id"`
	IndexerName string         `json:"indexer_name"`
	Sources     []SearchSource `json:"sources"`
	Categories  []int          `json:"categories"`
	SizeBytes   int64          `json:"size_bytes"`
	PublishDate string         `json:"publish_date"`
	Seeders     int            `json:"seeders"`
	// SeedersKnown=false 表示站点没给做种数，界面不能据此判定「无人做种」。
	SeedersKnown   bool     `json:"seeders_known"`
	Peers          int      `json:"peers"`
	Grabs          int      `json:"grabs"`
	DownloadType   string   `json:"download_type"`
	DownloadURL    string   `json:"download_url"`
	InfoHash       string   `json:"info_hash"`
	DetailsURL     string   `json:"details_url"`
	CategoryLabels []string `json:"category_labels"`
}

// SearchSource is one indexer variant of a merged card.
type SearchSource struct {
	IndexerID    uint   `json:"indexer_id"`
	IndexerName  string `json:"indexer_name"`
	DownloadURL  string `json:"download_url"`
	InfoHash     string `json:"info_hash"`
	Seeders      int    `json:"seeders"`
	SeedersKnown bool   `json:"seeders_known"`
	Peers        int    `json:"peers"`
	Grabs        int    `json:"grabs"`
	SizeBytes    int64  `json:"size_bytes"`
	DetailsURL   string `json:"details_url"`
}

// SiteQuery records the keyword one indexer was actually asked for. The UI
// shows it as a subtitle so the user can see why a site returned (or did not
// return) results for a Chinese query.
type SiteQuery struct {
	IndexerID   uint   `json:"indexer_id"`
	IndexerName string `json:"indexer_name"`
	Keyword     string `json:"keyword"`
	Language    string `json:"language"`
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
	// SiteQueries lists the keyword sent to each site, so the UI can show the
	// per-site search term instead of guessing.
	SiteQueries []SiteQuery `json:"site_queries,omitempty"`
	// Title is the alias resolution behind this query, so the UI can show the
	// confirmed title and offer the candidate list for manual confirmation.
	Title *TitleResolution `json:"title,omitempty"`
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
	// MediaType narrows alias resolution to movie or tv.
	MediaType string
	// Aliases overrides resolution with titles the user picked explicitly.
	Aliases []string
	// Confirmed marks Aliases as a manual choice, so no list is offered.
	Confirmed bool
	// RefreshTitles re-asks TMDB even when a cached resolution exists.
	RefreshTitles bool
	// SkipTitle turns alias resolution off for this query, so the raw keyword
	// goes to every site. The search page exposes this as a quick toggle for
	// "直接搜索" vs "片名搜索".
	SkipTitle bool
	// SiteLanguages restricts the fan-out to sites publishing in these
	// languages. Empty means every enabled site, which is the default. The
	// search page lets the user pick, so an English-only show can be searched
	// on English trackers and a Korean drama on Korean ones.
	SiteLanguages []string
}

// SearchService fans out a query across enabled indexers with global and
// per-site concurrency limits, then dedupes, filters, sorts and paginates.
type SearchService struct {
	repo      repository.SearchRepository
	indexers  *IndexerService
	cfg       *config.Config
	metrics   *SearchMetrics
	titles    *TitleAliasService
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
	return NewSearchServiceWithTitles(repo, indexers, cfg, nil)
}

// NewSearchServiceWithTitles wires title alias resolution into the search
// pipeline. A nil resolver keeps the previous keyword-only behaviour.
func NewSearchServiceWithTitles(repo repository.SearchRepository, indexers *IndexerService, cfg *config.Config, titles *TitleAliasService) *SearchService {
	limit := 3
	if cfg != nil && cfg.Search.MaxGlobalSearches > 0 {
		limit = cfg.Search.MaxGlobalSearches
	}
	return &SearchService{
		repo: repo, indexers: indexers, cfg: cfg, metrics: NewSearchMetrics(), titles: titles,
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
	// 40 lets a large library (80+ sites) resolve in two waves instead of four.
	// It is only a local gate: each site still honours its own request interval,
	// and one in-flight request per site is enforced by acquireSite.
	return 40
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

	indexers, err := s.candidatesFor(q.IndexerIDs, q.SiteLanguages)
	if err != nil {
		s.metrics.RecordSearch(time.Since(start), true)
		return nil, err
	}
	if len(indexers) == 0 {
		s.metrics.RecordSearch(time.Since(start), true)
		return nil, ErrNoIndexers
	}

	// Resolve the keyword into the titles indexers actually publish before the
	// fan-out. A failure here degrades to the raw keyword instead of blocking.
	resolution := s.resolveTitle(waitCtx, q)

	searchCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	type outcome struct {
		indexer  entity.SearchIndexer
		releases []connector.Release
		err      error
		skipped  bool
		// keywords are the terms actually sent, so relevance checks judge the
		// batch against what the site was asked.
		keywords []string
		// language is the site's primary search language, for the UI hint.
		language string
	}

	results := make(chan outcome, len(indexers))
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.maxWorkers())
	// Site language decides which alias to send: English trackers cannot answer
	// a Chinese title.
	languages := s.indexers.Languages(indexers)

	queueStart := time.Now()
	for _, idx := range indexers {
		wg.Add(1)
		go func(idx entity.SearchIndexer) {
			defer wg.Done()
			// Resolve the keywords up front so even a skipped or failed site
			// reports which terms it was asked for.
			keywords := siteKeywords(idx, q.Keyword, resolution, languages[idx.ID])
			skipOutcome := func(err *connector.Error) outcome {
				return outcome{indexer: idx, skipped: true, keywords: keywords, language: languages[idx.ID], err: err}
			}
			// Worker pool: a site that cannot be scheduled within 8s is skipped.
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-time.After(s.siteTimeout()):
				results <- skipOutcome(&connector.Error{Kind: connector.KindTimeout, Message: "站点排队超时"})
				return
			}
			if time.Since(queueStart) > s.siteTimeout() {
				results <- skipOutcome(&connector.Error{Kind: connector.KindTimeout, Message: "站点排队超时"})
				return
			}

			release, ok := s.acquireSite(searchCtx, idx.ID)
			if !ok {
				results <- skipOutcome(&connector.Error{Kind: connector.KindTimeout, Message: "站点排队超时"})
				return
			}
			defer release()

			conn, err := s.indexers.Connector(searchCtx, idx.ID)
			if err != nil {
				results <- outcome{indexer: idx, keywords: keywords, language: languages[idx.ID], err: err}
				return
			}
			if aware, ok := conn.(interface{ SupportsCategories([]int) bool }); ok && !aware.SupportsCategories(q.Categories) {
				// AC-27: do not query a site that declares no support for the
				// requested category. It is not a failure, just not applicable.
				results <- outcome{indexer: idx, skipped: true, keywords: keywords, language: languages[idx.ID]}
				return
			}

			// One request per configured language, run together so a site that
			// indexes several languages costs one round trip rather than N.
			var (
				mu        sync.Mutex
				collected []connector.Release
				firstErr  error
			)
			var keywordWG sync.WaitGroup
			for _, keyword := range keywords {
				keywordWG.Add(1)
				go func(keyword string) {
					defer keywordWG.Done()
					releases, err := safeSiteSearch(conn, searchCtx, connector.Query{
						Keyword: keyword, Categories: q.Categories, Season: q.Season,
						Episode: q.Episode, Year: q.Year, IMDBID: q.IMDBID, TMDBID: q.TMDBID,
						Page: q.Page - 1, Limit: MaxPageSize, Mode: q.Mode,
					}, idx.Name)
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						if firstErr == nil {
							firstErr = err
						}
						return
					}
					collected = append(collected, releases...)
				}(keyword)
			}
			keywordWG.Wait()

			if len(collected) == 0 && firstErr != nil {
				results <- outcome{indexer: idx, keywords: keywords, language: languages[idx.ID], err: firstErr}
				return
			}
			for i := range collected {
				collected[i].IndexerID = idx.ID
				collected[i].IndexerName = idx.Name
			}
			results <- outcome{indexer: idx, releases: collected, keywords: keywords, language: languages[idx.ID]}
		}(idx)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var all []connector.Release
	failures := make([]SearchFailure, 0)
	siteQueries := make([]SiteQuery, 0, len(indexers))
	skipped := 0
	irrelevant := 0
	for res := range results {
		// Record the keyword per site even when the site failed, so the UI can
		// explain what was asked.
		for _, keyword := range res.keywords {
			siteQueries = append(siteQueries, SiteQuery{
				IndexerID:   res.indexer.ID,
				IndexerName: res.indexer.Name,
				Keyword:     keyword,
				Language:    res.language,
			})
		}
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
		kept, dropped := connector.DropIrrelevantBatchFor(res.releases, relevanceKeywordsFor(res.keywords, resolution))
		if dropped {
			irrelevant++
			failures = append(failures, SearchFailure{
				IndexerID:   res.indexer.ID,
				IndexerName: res.indexer.Name,
				Kind:        "irrelevant",
				Message:     irrelevantReason(res.keywords[0]),
			})
			continue
		}
		// A site's category is not always trustworthy: some file hosts label a
		// cracked application as a movie. Drop the individual non-video entries
		// so a mixed batch still contributes its real episodes.
		kept = connector.DropNonVideoReleases(kept)
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
	// The alias table grows with every new keyword; sweep it on the same path so
	// no separate timer is needed. PruneStale throttles itself.
	if s.titles != nil {
		s.titles.PruneStale(time.Now())
	}

	sort.SliceStable(failures, func(i, j int) bool { return failures[i].IndexerID < failures[j].IndexerID })
	sort.SliceStable(siteQueries, func(i, j int) bool { return siteQueries[i].IndexerID < siteQueries[j].IndexerID })

	s.metrics.RecordSearch(time.Since(start), false)

	return &SearchResult{
		SearchID:    fmt.Sprintf("s_%d", start.UnixNano()),
		RequestID:   q.RequestID,
		ElapsedMS:   time.Since(start).Milliseconds(),
		Total:       len(items),
		TotalFound:  totalFound,
		Truncated:   truncated,
		Failures:    failures,
		Items:       items,
		Page:        q.Page,
		PageSize:    q.PageSize,
		Skipped:     skipped,
		Irrelevant:  irrelevant,
		Title:       resolution,
		SiteQueries: siteQueries,
	}, nil
}

// acquireSite enforces one in-flight request per indexer (BR-29). Waiting is
// bounded by 15s or the remaining search budget, whichever is shorter.
// safeSiteSearch runs one site's search and turns a panic into a per-site
// failure. A definition's template or filter bug must not take down the whole
// fan-out (which would also kill every other site's result).
func safeSiteSearch(conn connector.Connector, ctx context.Context, q connector.Query, name string) (releases []connector.Release, err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("search: 站点 %s 搜索时崩溃: %v", name, r)
			releases = nil
			err = &connector.Error{
				Kind:    connector.KindParse,
				Message: fmt.Sprintf("该站点定义处理失败（%v）", r),
			}
		}
	}()
	return conn.Search(ctx, q)
}

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
	return s.candidatesFor(ids, nil)
}

// candidatesFor is candidates with an optional site-language filter. An empty
// language list means no restriction.
func (s *SearchService) candidatesFor(ids []uint, languages []string) ([]entity.SearchIndexer, error) {
	enabled, err := s.repo.EnabledIndexers()
	if err != nil {
		return nil, err
	}
	wanted := map[uint]bool{}
	for _, id := range ids {
		wanted[id] = true
	}

	wantLang := normaliseLanguageSet(languages)

	out := make([]entity.SearchIndexer, 0, len(enabled))
	now := time.Now()
	for _, idx := range enabled {
		if len(wanted) > 0 && !wanted[idx.ID] {
			continue
		}
		// Language filtering happens here, before the fan-out, so a restricted
		// search does not waste a request on a site that cannot answer.
		//
		// The site's own SearchLanguages override decides: a site whose
		// definition says en-US but is configured to search zh+en genuinely
		// supports Chinese, so filtering on the definition language alone would
		// wrongly exclude it.
		if len(wantLang) > 0 && !siteSupportsAnyLanguage(idx, s.indexers.DefinitionLanguage(idx), wantLang) {
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

// siteSupportsAnyLanguage reports whether a site answers in any of the requested
// language groups. A configured SearchLanguages override wins over the
// definition's declared language, because that is what the search actually uses.
func siteSupportsAnyLanguage(idx entity.SearchIndexer, definitionLang string, wantLang map[string]bool) bool {
	langs := splitLanguages(idx.SearchLanguages)
	if len(langs) == 0 {
		langs = []string{definitionLang}
	}
	for _, lang := range langs {
		if wantLang[languageGroup(lang)] {
			return true
		}
	}
	return false
}

// normaliseLanguageSet lower-cases and de-duplicates the requested languages.
func normaliseLanguageSet(languages []string) map[string]bool {
	out := map[string]bool{}
	for _, lang := range languages {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if lang == "" || lang == "all" {
			continue
		}
		out[languageGroup(lang)] = true
	}
	return out
}

// languageGroup maps a BCP-47 tag to its primary subtag, so "zh-CN", "zh-TW" and
// "zh" all compare equal while staying distinct from other languages.
func languageGroup(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return ""
	}
	if idx := strings.Index(lang, "-"); idx > 0 {
		return lang[:idx]
	}
	return lang
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
			IndexerID:    src.IndexerID,
			IndexerName:  src.IndexerName,
			DownloadURL:  src.DownloadURL,
			InfoHash:     src.InfoHash,
			Seeders:      src.Seeders,
			SeedersKnown: src.SeedersKnown,
			Grabs:        src.Grabs,
			SizeBytes:    src.SizeBytes,
			DetailsURL:   src.DetailsURL,
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
		SeedersKnown:   card.SeedersKnown,
		Peers:          card.Peers,
		Grabs:          card.Grabs,
		DownloadType:   card.DownloadType,
		DownloadURL:    card.DownloadURL,
		InfoHash:       card.InfoHash,
		DetailsURL:     card.DetailsURL,
		CategoryLabels: card.CategoryNames,
	}
}

// resolveTitle turns the user keyword into the titles indexers publish. A
// failure degrades to the raw keyword: alias resolution is an optimisation, not
// a dependency.
func (s *SearchService) resolveTitle(ctx context.Context, q SearchQuery) *TitleResolution {
	// The caller asked for a plain keyword search: do not consult TMDB and do
	// not rewrite the term for any site.
	if q.SkipTitle {
		return nil
	}
	// A confirmed choice in the cache carries the candidate plus every alias
	// with its language tag, which is what per-site keyword routing needs. Prefer
	// it over a bare alias list.
	if s.titles != nil && s.titles.Enabled() {
		if cached := s.titles.Cached(q.Keyword, q.MediaType); cached != nil && cached.Confirmed && cached.Selected != nil {
			return cached
		}
	}
	if len(q.Aliases) > 0 {
		return &TitleResolution{
			Keyword: q.Keyword, MediaType: q.MediaType, Aliases: q.Aliases, Confirmed: q.Confirmed,
		}
	}
	if s.titles == nil || !s.titles.Enabled() {
		return nil
	}
	if q.RefreshTitles {
		resolution, err := s.titles.Resolve(ctx, q.Keyword, q.MediaType, true)
		if err != nil {
			logger.Warn("search: title resolution failed: %v", err)
			return nil
		}
		return resolution
	}
	if cached := s.titles.Cached(q.Keyword, q.MediaType); cached != nil {
		// A confirmed choice is authoritative. An unconfirmed one is reused but
		// still surfaced so the user can correct it.
		return cached
	}
	resolution, err := s.titles.Resolve(ctx, q.Keyword, q.MediaType, false)
	if err != nil {
		logger.Warn("search: title resolution failed: %v", err)
		return nil
	}
	return resolution
}

// relevanceKeywordsFor is relevanceKeywords for a site queried in several
// languages: every keyword actually sent counts, plus the original term.
func relevanceKeywordsFor(sent []string, resolution *TitleResolution) []string {
	out := make([]string, 0, len(sent)+4)
	add := func(title string) {
		title = strings.TrimSpace(title)
		if title == "" {
			return
		}
		for _, existing := range out {
			if strings.EqualFold(existing, title) {
				return
			}
		}
		out = append(out, title)
	}
	for _, keyword := range sent {
		add(keyword)
	}
	if resolution != nil {
		add(resolution.Keyword)
		for _, alias := range resolution.Aliases {
			add(alias)
		}
	}
	return out
}

// relevanceKeywords is the set of titles a site's batch may match. The alias we
// sent always counts, plus the original keyword, so a site that answered with
// either is kept.
func relevanceKeywords(sent string, resolution *TitleResolution) []string {
	return relevanceKeywordsFor([]string{sent}, resolution)
}

// siteKeywords returns every keyword this site should be searched with.
//
// A site normally answers in one language, so this is a single keyword. When the
// user configured several languages for it (FileMood indexes both Chinese and
// English releases), the site is queried once per language and the results are
// merged, which finds more than picking one language.
func siteKeywords(idx entity.SearchIndexer, base string, resolution *TitleResolution, definitionLang string) []string {
	langs := splitLanguages(idx.SearchLanguages)
	if len(langs) == 0 {
		langs = []string{definitionLang}
	}
	out := make([]string, 0, len(langs))
	seen := map[string]bool{}
	for _, lang := range langs {
		keyword := keywordFor(resolution, base, lang)
		if strings.TrimSpace(keyword) == "" {
			continue
		}
		key := strings.ToLower(keyword)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, keyword)
	}
	if len(out) == 0 {
		return []string{base}
	}
	return out
}

// keywordFor picks the alias for one language. It prefers the selected
// candidate, which carries language tags. When only a bare alias list is
// available (a caller supplied aliases directly), it falls back to picking by
// writing system — and skips transliterations, which a tracker never indexes.
func keywordFor(resolution *TitleResolution, base, lang string) string {
	if resolution != nil && resolution.Selected != nil {
		return KeywordForLanguage(resolution, base, lang)
	}
	if resolution == nil || len(resolution.Aliases) == 0 {
		return base
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	if strings.HasPrefix(lang, "zh") {
		return base
	}
	wantHangul := lang == "ko" || strings.HasPrefix(lang, "ko-")
	wantKana := strings.HasPrefix(lang, "ja")
	for _, alias := range resolution.Aliases {
		switch {
		case wantHangul && isHangul(alias):
			return alias
		case wantKana && isKana(alias):
			return alias
		case !wantHangul && !wantKana && !containsCJK(alias) && containsLatin(alias) && !isTransliteration(alias):
			return alias
		}
	}
	return base
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

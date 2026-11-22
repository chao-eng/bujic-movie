package service

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/bujic-movie/bujic-movie/pkg/tmdb"
)

// AliasSource labels where a title came from, so the UI can explain itself.
const (
	AliasSourceLocalized    = "localized"
	AliasSourceOriginal     = "original"
	AliasSourceAlternative  = "alternative"
	AliasSourceManual       = "manual"
)

// TitleAliasEntry is one title variant with the language it belongs to.
type TitleAliasEntry struct {
	Title  string `json:"title"`
	Lang   string `json:"lang"`
	Source string `json:"source"`
}

// TitleCandidate is one TMDB match offered for manual confirmation.
type TitleCandidate struct {
	TMDBID        int               `json:"tmdb_id"`
	MediaType     string            `json:"media_type"` // movie / tv
	Title         string            `json:"title"`
	OriginalTitle string            `json:"original_title"`
	Year          int               `json:"year"`
	Overview      string            `json:"overview"`
	PosterPath    string            `json:"poster_path"`
	VoteAverage   float64           `json:"vote_average"`
	VoteCount     int               `json:"vote_count"`
	Popularity    float64           `json:"popularity"`
	Aliases       []TitleAliasEntry `json:"aliases"`
	// MatchRank is how well this candidate answers the keyword (higher is
	// better). It is internal scoring, exposed so the UI could explain a pick.
	MatchRank int `json:"match_rank"`
}

// TitleResolution is the outcome of turning a user keyword into indexer queries.
type TitleResolution struct {
	Keyword  string `json:"keyword"`
	MediaType string `json:"media_type"`
	// Selected is the candidate currently driving the search.
	Selected *TitleCandidate `json:"selected,omitempty"`
	// Candidates is the list shown for manual confirmation.
	Candidates []TitleCandidate `json:"candidates"`
	// Aliases are the titles actually queried, in priority order.
	Aliases []string `json:"aliases"`
	// NeedsConfirmation is true when the match was picked automatically and the
	// user has not confirmed it yet.
	NeedsConfirmation bool `json:"needs_confirmation"`
	Confirmed         bool `json:"confirmed"`
	Cached            bool `json:"cached"`
	// Degraded means TMDB could not be consulted; the raw keyword is used.
	Degraded bool   `json:"degraded"`
	Message  string `json:"message,omitempty"`
}

// TitleAliasService resolves Chinese titles to the names indexers actually use.
// TMDB is the only provider: it already carries the local and original titles
// plus market-specific alternatives, and needs no scraping.
type TitleAliasService struct {
	repo   repository.SearchRepository
	client *tmdb.Client
	cfg    *config.Config

	mu       sync.Mutex
	inFlight map[string]*sync.Mutex
}

// NewTitleAliasService wires the resolver.
func NewTitleAliasService(repo repository.SearchRepository, client *tmdb.Client, cfg *config.Config) *TitleAliasService {
	return &TitleAliasService{repo: repo, client: client, cfg: cfg, inFlight: map[string]*sync.Mutex{}}
}

// Enabled reports whether alias resolution should run at all.
func (s *TitleAliasService) Enabled() bool {
	return s != nil && s.client != nil && s.cfg != nil && s.cfg.Search.AliasResolution
}

func (s *TitleAliasService) timeout() time.Duration {
	if s.cfg != nil && s.cfg.Search.AliasTimeoutMS > 0 {
		return time.Duration(s.cfg.Search.AliasTimeoutMS) * time.Millisecond
	}
	return 4000 * time.Millisecond
}

// cacheTTL is how long a confirmed resolution is trusted.
const aliasCacheTTL = 30 * 24 * time.Hour

// Resolve returns aliases for keyword. forceRefresh bypasses the cache, which is
// what the UI uses when the user rejects the automatic pick.
func (s *TitleAliasService) Resolve(ctx context.Context, keyword, mediaType string, forceRefresh bool) (*TitleResolution, error) {
	keyword = NormaliseKeyword(keyword)
	mediaType = normaliseMediaType(mediaType)
	if keyword == "" {
		return &TitleResolution{Keyword: keyword}, nil
	}
	if !s.Enabled() {
		return &TitleResolution{Keyword: keyword, Aliases: []string{keyword}, Degraded: true, Message: "片名解析未启用，按原关键词搜索"}, nil
	}

	unlock := s.lock(keyword + "|" + mediaType)
	defer unlock()

	return s.resolveLocked(ctx, keyword, mediaType, forceRefresh)
}

// resolveLocked is Resolve without the per-keyword lock. Callers that already
// hold it (Confirm) must use this to avoid self-deadlocking on the same mutex.
func (s *TitleAliasService) resolveLocked(ctx context.Context, keyword, mediaType string, forceRefresh bool) (*TitleResolution, error) {
	// A forced re-resolution discards every stored variant for this keyword,
	// including rows written under a different media type. Without this, a
	// confirmation stored as (keyword, tv) survives a re-resolve stored as
	// (keyword, "") and keeps being picked up by later cached lookups.
	if forceRefresh {
		if err := s.repo.DeleteTitleAlias(keyword); err != nil {
			logger.Warn("search: clearing title alias for %q failed: %v", keyword, err)
		}
	}

	if !forceRefresh {
		if cached, err := s.repo.GetTitleAlias(keyword, mediaType); err == nil && !cached.Stale {
			if resolution := s.fromCache(cached); resolution != nil {
				if cached.Confirmed || time.Since(cached.UpdatedAt) < aliasCacheTTL {
					return resolution, nil
				}
			}
		}
	}

	lookupCtx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	candidates, err := s.lookup(lookupCtx, keyword, mediaType)
	if err != nil {
		logger.Warn("search: title alias lookup failed for %q: %v", keyword, err)
		// A failed lookup must never block the search: fall back to the raw
		// keyword, but keep any previously stored candidates so the UI still
		// offers the list.
		resolution := &TitleResolution{
			Keyword: keyword, MediaType: mediaType, Aliases: []string{keyword},
			Degraded: true, Message: "TMDB 查询失败，按原关键词搜索",
		}
		if cached, cacheErr := s.repo.GetTitleAlias(keyword, mediaType); cacheErr == nil {
			if fromCache := s.fromCache(cached); fromCache != nil {
				fromCache.Degraded = true
				fromCache.Message = resolution.Message
				return fromCache, nil
			}
		}
		return resolution, nil
	}

	if len(candidates) == 0 {
		resolution := &TitleResolution{
			Keyword: keyword, MediaType: mediaType, Aliases: []string{keyword},
			Message: "TMDB 未找到匹配的影视剧，按原关键词搜索",
		}
		s.persist(entity.SearchTitleAlias{
			Keyword: keyword, MediaType: mediaType, AliasesJSON: mustJSON([]string{keyword}),
			CandidatesJSON: mustJSON([]TitleCandidate{}), Stale: true,
		})
		return resolution, nil
	}

	selected := pickCandidate(candidates, keyword)
	resolution := &TitleResolution{
		Keyword: keyword, MediaType: mediaType, Candidates: candidates,
		Selected: selected, Aliases: aliasesOf(selected, keyword),
		NeedsConfirmation: true,
	}
	s.persist(entity.SearchTitleAlias{
		Keyword: keyword, MediaType: mediaType,
		TMDBID: selected.TMDBID, Title: selected.Title, OriginalTitle: selected.OriginalTitle,
		Year: selected.Year, AliasesJSON: mustJSON(resolution.Aliases),
		CandidatesJSON: mustJSON(candidates),
	})
	return resolution, nil
}

// Confirm stores the user's choice and returns the resolution built from it.
func (s *TitleAliasService) Confirm(ctx context.Context, keyword, mediaType string, tmdbID int) (*TitleResolution, error) {
	keyword = NormaliseKeyword(keyword)
	mediaType = normaliseMediaType(mediaType)
	if keyword == "" {
		return nil, errors.New("关键词为空")
	}

	unlock := s.lock(keyword + "|" + mediaType)
	defer unlock()

	cached, err := s.repo.GetTitleAlias(keyword, mediaType)
	if err != nil || cached == nil {
		// Nothing cached under this media type: resolve first so a confirm from an
		// old UI does not fabricate a selection. Call the locked-free variant
		// because this method already holds the per-keyword mutex.
		resolution, resolveErr := s.resolveLocked(ctx, keyword, mediaType, true)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if resolution == nil || len(resolution.Candidates) == 0 {
			return nil, errors.New("没有可确认的候选，请重新搜索")
		}
		if selected := findCandidate(resolution.Candidates, tmdbID); selected != nil {
			return s.saveConfirmed(keyword, mediaType, resolution.Candidates, selected)
		}
		return nil, errors.New("候选已失效，请重新搜索")
	}

	candidates := decodeCandidates(cached.CandidatesJSON)
	selected := findCandidate(candidates, tmdbID)
	if selected == nil {
		return nil, errors.New("候选中没有该影视剧，请刷新候选列表")
	}
	return s.saveConfirmed(keyword, mediaType, candidates, selected)
}

// findCandidate returns the candidate with the given TMDB id, or nil.
func findCandidate(candidates []TitleCandidate, tmdbID int) *TitleCandidate {
	for i := range candidates {
		if candidates[i].TMDBID == tmdbID {
			return &candidates[i]
		}
	}
	return nil
}

// saveConfirmed persists the user's choice. The caller must hold the keyword
// lock.
func (s *TitleAliasService) saveConfirmed(keyword, mediaType string, candidates []TitleCandidate, selected *TitleCandidate) (*TitleResolution, error) {
	cached, err := s.repo.GetTitleAlias(keyword, mediaType)
	if err != nil || cached == nil {
		cached = &entity.SearchTitleAlias{Keyword: keyword, MediaType: mediaType}
	}
	cached.TMDBID = selected.TMDBID
	cached.Title = selected.Title
	cached.OriginalTitle = selected.OriginalTitle
	cached.Year = selected.Year
	cached.Confirmed = true
	cached.Stale = false
	aliases := aliasesOf(selected, keyword)
	cached.AliasesJSON = mustJSON(aliases)
	cached.CandidatesJSON = mustJSON(candidates)
	if err := s.repo.SaveTitleAlias(cached); err != nil {
		return nil, err
	}
	return &TitleResolution{
		Keyword: keyword, MediaType: mediaType, Candidates: candidates,
		Selected: selected, Aliases: aliases, Confirmed: true, Cached: true,
	}, nil
}

// Clear drops the cached resolution so the next search re-asks TMDB.
func (s *TitleAliasService) Clear(keyword string) error {
	return s.repo.DeleteTitleAlias(NormaliseKeyword(keyword))
}

// Cached returns the stored resolution without any network access. Search uses
// it so a confirmed choice applies to every later query.
func (s *TitleAliasService) Cached(keyword, mediaType string) *TitleResolution {
	if s == nil || s.repo == nil {
		return nil
	}
	cached, err := s.repo.GetTitleAlias(NormaliseKeyword(keyword), normaliseMediaType(mediaType))
	if err != nil {
		return nil
	}
	return s.fromCache(cached)
}

func (s *TitleAliasService) fromCache(cached *entity.SearchTitleAlias) *TitleResolution {
	if cached == nil {
		return nil
	}
	aliases := decodeStrings(cached.AliasesJSON)
	var selected *TitleCandidate
	candidates := decodeCandidates(cached.CandidatesJSON)
	for i := range candidates {
		if candidates[i].TMDBID == cached.TMDBID {
			selected = &candidates[i]
			break
		}
	}
	if selected == nil && cached.TMDBID != 0 {
		selected = &TitleCandidate{
			TMDBID: cached.TMDBID, MediaType: cached.MediaType,
			Title: cached.Title, OriginalTitle: cached.OriginalTitle, Year: cached.Year,
		}
	}
	if len(aliases) == 0 {
		aliases = []string{cached.Keyword}
	}
	return &TitleResolution{
		Keyword: cached.Keyword, MediaType: cached.MediaType, Candidates: candidates,
		Selected: selected, Aliases: aliases, Confirmed: cached.Confirmed,
		NeedsConfirmation: !cached.Confirmed && len(candidates) > 0,
		Cached:            true,
	}
}

func (s *TitleAliasService) persist(alias entity.SearchTitleAlias) {
	if s.repo == nil || alias.Keyword == "" {
		return
	}
	if err := s.repo.SaveTitleAlias(&alias); err != nil {
		logger.Warn("search: storing title alias failed: %v", err)
	}
}

// lookup asks TMDB for both media types when the caller did not pin one. The two
// searches run concurrently, and alternative titles are fetched only for the
// most promising candidates so the whole resolution stays within two round
// trips: a serial version easily exceeded the budget on a normal connection.
func (s *TitleAliasService) lookup(ctx context.Context, keyword, mediaType string) ([]TitleCandidate, error) {
	type searchResult struct {
		movie []tmdb.SearchMovieResult
		tv    []tmdb.SearchTVResult
		err   error
	}

	wantMovie := mediaType == "" || mediaType == "movie"
	wantTV := mediaType == "" || mediaType == "tv"

	var (
		movieRes, tvRes searchResult
		wg              sync.WaitGroup
	)
	if wantMovie {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := s.client.SearchMovie(ctx, keyword, 0)
			movieRes = searchResult{movie: results, err: err}
		}()
	}
	if wantTV {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := s.client.SearchTV(ctx, keyword, 0)
			tvRes = searchResult{tv: results, err: err}
		}()
	}
	wg.Wait()

	if movieRes.err != nil && tvRes.err != nil {
		return nil, movieRes.err
	}

	// Rank on the search payload alone (title + original title already reveal
	// exact matches), so regional alternatives only decorate the shortlist.
	shortlist := make([]TitleCandidate, 0, 6)
	if movieRes.err == nil {
		shortlist = append(shortlist, s.movieCandidates(nil, movieRes.movie, keyword)...)
	}
	if tvRes.err == nil {
		shortlist = append(shortlist, s.tvCandidates(nil, tvRes.tv, keyword)...)
	}
	sortCandidates(shortlist)
	// Keep a few extra before alias enrichment: the true match can rank low until
	// its regional titles are known, then it wins on the second sort.
	if len(shortlist) > 8 {
		shortlist = shortlist[:8]
	}
	if len(shortlist) == 0 {
		return nil, nil
	}

	// Fetch alternatives for the whole shortlist in parallel, then rank again.
	// Skipping this for "decisive" hits is wrong: a Chinese show's English name
	// ("Squid Game" for 鱿鱼游戏) exists only in the alternatives payload, so a
	// latin keyword otherwise matches nothing.
	top := shortlist
	var altWG sync.WaitGroup
	for i := range top {
		altWG.Add(1)
		go func(index int) {
			defer altWG.Done()
			candidate := &top[index]
			if candidate.MediaType == "movie" {
				resp, err := s.client.GetMovieAlternativeTitles(ctx, candidate.TMDBID)
				if err != nil {
					return
				}
				for _, alt := range resp.Titles {
					candidate.Aliases = append(candidate.Aliases, TitleAliasEntry{
						Title: alt.Title, Lang: strings.ToLower(alt.ISO3166_1), Source: AliasSourceAlternative,
					})
				}
			} else {
				resp, err := s.client.GetTVAlternativeTitles(ctx, candidate.TMDBID)
				if err != nil {
					return
				}
				for _, alt := range resp.Results {
					candidate.Aliases = append(candidate.Aliases, TitleAliasEntry{
						Title: alt.Title, Lang: strings.ToLower(alt.ISO3166_1), Source: AliasSourceAlternative,
					})
				}
			}
			candidate.Aliases = dedupeAliases(candidate.Aliases)
			candidate.MatchRank = matchRank(*candidate, keyword)
		}(i)
	}
	altWG.Wait()

	sortCandidates(shortlist)
	if len(shortlist) > 5 {
		shortlist = shortlist[:5]
	}
	return shortlist, nil
}

// movieCandidates builds the shortlist entries for movie search hits. Regional
// alternatives are loaded later, only for the candidates that survive ranking.
func (s *TitleAliasService) movieCandidates(_ context.Context, results []tmdb.SearchMovieResult, keyword string) []TitleCandidate {
	out := make([]TitleCandidate, 0, len(results))
	for _, r := range results {
		candidate := TitleCandidate{
			TMDBID: r.ID, MediaType: "movie", Title: r.Title,
			OriginalTitle: r.OriginalTitle, Year: yearOf(r.ReleaseDate),
			Overview: r.Overview, PosterPath: r.PosterPath, VoteAverage: r.VoteAverage,
			VoteCount: r.VoteCount, Popularity: r.Popularity,
			Aliases: []TitleAliasEntry{{Title: r.Title, Lang: "localized", Source: AliasSourceLocalized}},
		}
		if r.OriginalTitle != "" {
			candidate.Aliases = append(candidate.Aliases, TitleAliasEntry{Title: r.OriginalTitle, Lang: "original", Source: AliasSourceOriginal})
		}
		candidate.Aliases = dedupeAliases(candidate.Aliases)
		candidate.MatchRank = matchRank(candidate, keyword)
		out = append(out, candidate)
	}
	return out
}

// tvCandidates is the TV counterpart of movieCandidates.
func (s *TitleAliasService) tvCandidates(_ context.Context, results []tmdb.SearchTVResult, keyword string) []TitleCandidate {
	out := make([]TitleCandidate, 0, len(results))
	for _, r := range results {
		candidate := TitleCandidate{
			TMDBID: r.ID, MediaType: "tv", Title: r.Name,
			OriginalTitle: r.OriginalName, Year: yearOf(r.FirstAirDate),
			Overview: r.Overview, PosterPath: r.PosterPath, VoteAverage: r.VoteAverage,
			VoteCount: r.VoteCount, Popularity: r.Popularity,
			Aliases: []TitleAliasEntry{{Title: r.Name, Lang: "localized", Source: AliasSourceLocalized}},
		}
		if r.OriginalName != "" {
			candidate.Aliases = append(candidate.Aliases, TitleAliasEntry{Title: r.OriginalName, Lang: "original", Source: AliasSourceOriginal})
		}
		candidate.Aliases = dedupeAliases(candidate.Aliases)
		candidate.MatchRank = matchRank(candidate, keyword)
		out = append(out, candidate)
	}
	return out
}

// pickCandidate picks the best answer. Exact title equality wins, then a title
// that begins with the keyword, then a title that merely contains it; ties break
// on TMDB popularity so a spin-off with few votes never beats the original.
func pickCandidate(candidates []TitleCandidate, keyword string) *TitleCandidate {
	if len(candidates) == 0 {
		return nil
	}
	best := &candidates[0]
	for i := range candidates {
		if candidates[i].MatchRank > best.MatchRank ||
			(candidates[i].MatchRank == best.MatchRank && candidates[i].VoteCount > best.VoteCount) ||
			(candidates[i].MatchRank == best.MatchRank && candidates[i].VoteCount == best.VoteCount && candidates[i].Popularity > best.Popularity) {
			best = &candidates[i]
		}
	}
	return best
}

// sortCandidates orders the list the way the confirmation UI should read it:
// best match first, then the most established production.
func sortCandidates(candidates []TitleCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].MatchRank != candidates[j].MatchRank {
			return candidates[i].MatchRank > candidates[j].MatchRank
		}
		if candidates[i].VoteCount != candidates[j].VoteCount {
			return candidates[i].VoteCount > candidates[j].VoteCount
		}
		return candidates[i].Popularity > candidates[j].Popularity
	})
}

// matchRank scores how well a candidate answers the keyword. It exists because
// TMDB returns spin-offs and special editions whose titles merely contain the
// query ("鱿鱼游戏马来版" for "鱿鱼游戏"), and picking the first hit is wrong.
func matchRank(candidate TitleCandidate, keyword string) int {
	keys := []string{strings.ToLower(strings.TrimSpace(keyword))}
	// A user may type the season along with the name; compare the bare title too.
	if bare := stripSeasonSuffix(keyword); bare != "" && !strings.EqualFold(bare, keyword) {
		keys = append(keys, strings.ToLower(bare))
	}

	best := 0
	for _, alias := range candidate.Aliases {
		title := strings.ToLower(strings.TrimSpace(alias.Title))
		if title == "" {
			continue
		}
		for _, key := range keys {
			if key == "" {
				continue
			}
			switch {
			case title == key:
				return 100
			case strings.HasPrefix(title, key):
				// Longer extra text means a qualifier ("…马来版", "…：真人挑战赛"),
				// so shorten the lead over a plain contains match.
				score := 70 - len([]rune(title)) + len([]rune(key))
				score = maxInt(score, 55)
				best = maxInt(best, score)
			case strings.Contains(title, key):
				best = maxInt(best, 40)
			}
		}
	}
	return best
}

// stripSeasonSuffix removes a trailing season marker so "鱿鱼游戏 第二季" is
// compared as "鱿鱼游戏". Numbered forms are handled too.
var seasonSuffixPattern = regexp.MustCompile(`(?i)[\s·:：-]*(第\s*[0-9一二三四五六七八九十百]+\s*季|season\s*[0-9]+|s[0-9]{1,2})$`)

func stripSeasonSuffix(keyword string) string {
	stripped := seasonSuffixPattern.ReplaceAllString(strings.TrimSpace(keyword), "")
	return strings.TrimSpace(stripped)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// aliasesOf flattens a candidate into distinct query titles, keeping the
// localized name first because some sites index it too.
func aliasesOf(candidate *TitleCandidate, keyword string) []string {
	if candidate == nil {
		return []string{keyword}
	}
	out := make([]string, 0, len(candidate.Aliases)+1)
	seen := map[string]bool{}
	add := func(title string) {
		title = strings.TrimSpace(title)
		if title == "" {
			return
		}
		key := strings.ToLower(title)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, title)
	}
	add(keyword)
	for _, alias := range candidate.Aliases {
		add(alias.Title)
	}
	return out
}

func dedupeAliases(entries []TitleAliasEntry) []TitleAliasEntry {
	out := make([]TitleAliasEntry, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			continue
		}
		key := strings.ToLower(title)
		if seen[key] {
			continue
		}
		seen[key] = true
		entry.Title = title
		out = append(out, entry)
	}
	return out
}

// KeywordForLanguage picks the alias most likely to be indexed by a site that
// publishes in the given language.
func KeywordForLanguage(resolution *TitleResolution, keyword, lang string) string {
	if resolution == nil || resolution.Selected == nil {
		return keyword
	}
	// A keyword the user already typed in a latin script is what latin sites
	// index; rewriting it to another market's title only narrows the query.
	if !containsCJK(keyword) && containsLatin(keyword) && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh") {
		return keyword
	}
	aliases := resolution.Selected.Aliases
	lang = strings.ToLower(strings.TrimSpace(lang))
	if strings.HasPrefix(lang, "zh") {
		// Chinese sites index the localized name.
		return keyword
	}
	if lang == "ko" || strings.HasPrefix(lang, "ko-") || strings.HasPrefix(lang, "ko_") {
		if title := firstWithScript(aliases, isHangul); title != "" {
			return title
		}
		if title := bestLatinAlias(aliases, lang); title != "" {
			return title
		}
		return keyword
	}
	if strings.HasPrefix(lang, "ja") {
		if title := firstWithScript(aliases, isKana); title != "" {
			return title
		}
		if title := bestLatinAlias(aliases, lang); title != "" {
			return title
		}
		return keyword
	}
	// English and other latin sites: prefer a title published in a latin-script
	// market over a transliteration. Pinyin ("Liú làng dì qiú", tagged cn) is
	// latin script but not what a tracker indexes, so score it below us/gb/en.
	if title := bestLatinAlias(aliases, lang); title != "" {
		return title
	}
	return keyword
}

// latinMarketPriority ranks where a latin-script title came from. A release
// title from an English-speaking market is the best bet; the original title and
// other latin markets come next; transliterations come last.
func latinMarketPriority(alias TitleAliasEntry, wantLang string) int {
	lang := strings.ToLower(strings.TrimSpace(alias.Lang))
	switch {
	case lang == wantLang, strings.HasPrefix(lang, wantLang+":"):
		return 4
	case lang == "us", lang == "gb", lang == "au", lang == "ca", lang == "nz":
		return 3
	case lang == "original", lang == "":
		return 2
	case lang == "ie", lang == "nz":
		return 2
	default:
		return 1
	}
}

// bestLatinAlias picks the latin title most likely to appear on the site. It
// returns "" when every latin alias is a transliteration, so callers fall back
// to the keyword instead of sending pinyin to a Japanese tracker.
func bestLatinAlias(aliases []TitleAliasEntry, wantLang string) string {
	best := ""
	bestScore := 0
	transliteration := ""
	for _, alias := range aliases {
		title := strings.TrimSpace(alias.Title)
		if title == "" || containsCJK(title) || !containsLatin(title) {
			continue
		}
		// A pinyin transliteration ("Liú làng dì qiú") is latin script but is not
		// a title a tracker publishes; keep it only as a last resort.
		if isTransliteration(title) {
			if transliteration == "" {
				transliteration = title
			}
			continue
		}
		score := latinMarketPriority(alias, wantLang)
		if score > bestScore {
			best = title
			bestScore = score
		}
	}
	if best != "" {
		return best
	}
	return transliteration
}

// isTransliteration spots pinyin/mixed-diacritic renderings of Chinese titles,
// which TMDB stores under the cn region but no tracker indexes.
func isTransliteration(title string) bool {
	// Pinyin is a space-separated run of short syllables with tons of diacritics.
	diacritics := 0
	for _, r := range title {
		if r > 127 && r < 0x2500 {
			diacritics++
		}
	}
	return diacritics >= 2
}

func containsLatin(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

// hasLatinAlias reports whether any alias is already a latin title. It is used
// by tests to assert the enriched alias set carries a latin form.
func hasLatinAlias(aliases []TitleAliasEntry) bool {
	for _, alias := range aliases {
		if containsLatin(alias.Title) && !containsCJK(alias.Title) && !isTransliteration(alias.Title) {
			return true
		}
	}
	return false
}

// firstWithScript returns the first alias whose title uses the given script.
func firstWithScript(aliases []TitleAliasEntry, match func(string) bool) string {
	for _, alias := range aliases {
		if match(alias.Title) {
			return alias.Title
		}
	}
	return ""
}

// firstNonCJK is the plain "any latin title" fallback.
func firstNonCJK(aliases []TitleAliasEntry) string {
	for _, alias := range aliases {
		if !containsCJK(alias.Title) && containsLatin(alias.Title) {
			return alias.Title
		}
	}
	return ""
}

func isHangul(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

func isKana(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			return true
		}
	}
	return false
}

func containsHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func containsCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			return true
		}
	}
	return false
}

func normaliseMediaType(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "movie":
		return "movie"
	case "tv":
		return "tv"
	}
	return ""
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	year := 0
	for _, r := range date[:4] {
		if r < '0' || r > '9' {
			return 0
		}
		year = year*10 + int(r-'0')
	}
	return year
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func decodeStrings(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func decodeCandidates(raw string) []TitleCandidate {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []TitleCandidate
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// lock serialises lookups for one keyword so a burst of identical searches only
// hits TMDB once.
func (s *TitleAliasService) lock(key string) func() {
	s.mu.Lock()
	mutex, ok := s.inFlight[key]
	if !ok {
		mutex = &sync.Mutex{}
		s.inFlight[key] = mutex
	}
	s.mu.Unlock()
	mutex.Lock()
	return func() {
		mutex.Unlock()
	}
}

// PruneCache drops resolutions that have not been touched in a long time.
func (s *TitleAliasService) PruneCache(before time.Time) error {
	if s == nil || s.repo == nil {
		return nil
	}
	return s.repo.DeleteTitleAliasesBefore(before)
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/config"
	internalcrypto "github.com/bujic-movie/bujic-movie/internal/crypto"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/service/cardigann"
	"github.com/bujic-movie/bujic-movie/internal/service/connector"
	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"gorm.io/gorm"
)

// IndexerInput is the API payload for creating or updating an indexer.
type IndexerInput struct {
	Name             string            `json:"name"`
	Connector        string            `json:"connector"`
	DefinitionID     string            `json:"definition_id"`
	BaseURL          string            `json:"base_url"`
	APIKey           string            `json:"api_key"`
	Settings         map[string]string `json:"settings"`
	Secrets          map[string]string `json:"secrets"`
	Categories       string            `json:"categories"`
	ProxyURL         string            `json:"proxy_url"`
	ExtraCookies     string            `json:"extra_cookies"`
	UserAgent        string            `json:"user_agent"`
	MinIntervalMS    int               `json:"min_interval_ms"`
	Priority         int               `json:"priority"`
	DownloadClientID uint              `json:"download_client_id"`
	Enabled          *bool             `json:"enabled"`
	// SearchLanguages lets a site be searched in several languages at once
	// ("zh", "en"). Empty follows the definition's declared language.
	SearchLanguages []string `json:"search_languages"`
}

// IndexerView is a read model that masks every secret.
type IndexerView struct {
	entity.SearchIndexer
	APIKeyMask       string             `json:"api_key_mask"`
	SecretsMask      map[string]string  `json:"secrets_mask"`
	ProxyMask        string             `json:"proxy_mask"`
	CookieNames      []string           `json:"cookie_names"`
	CookieMask       string             `json:"cookie_mask"`
	Status           *IndexerStatusView `json:"status,omitempty"`
	SessionState     string             `json:"session_state"`
	DefinitionStatus string             `json:"definition_status"`
	// SearchLanguages exposes the configured languages as a list. The entity
	// stores them comma separated and hides the raw column from JSON.
	SearchLanguages []string `json:"search_languages"`
	// SiteURL is the site's homepage so the list can link to it. Cardigann
	// sites take it from the definition's links; other connectors have no
	// reliable homepage, so it stays empty.
	SiteURL string `json:"site_url"`
}

// IndexerStatusView is the健康 summary shown in the UI.
type IndexerStatusView struct {
	LastSuccessAt       string `json:"last_success_at"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	CooldownUntil       string `json:"cooldown_until"`
	CooldownRemainingMS int64  `json:"cooldown_remaining_ms"`
	AvgElapsedMS        int    `json:"avg_elapsed_ms"`
	LastErrorKind       string `json:"last_error_kind"`
}

// TestResult is the outcome of a connectivity test.
type TestResult struct {
	OK               bool     `json:"ok"`
	IndexerName      string   `json:"indexer_name"`
	Protocol         string   `json:"protocol"`
	Categories       int      `json:"categories"`
	SearchModes      []string `json:"search_modes"`
	ElapsedMS        int64    `json:"elapsed_ms"`
	LoginState       string   `json:"login_state"`
	ErrorKind        string   `json:"error_kind,omitempty"`
	Message          string   `json:"message,omitempty"`
	Field            string   `json:"field,omitempty"`
	NeedsCaptcha     bool     `json:"needs_captcha"`
	CategoriesReady  []string `json:"categories_ready,omitempty"`
	CategoriesFailed []string `json:"categories_failed,omitempty"`
	// Status and SessionState let the UI patch the row in place after a test
	// instead of reloading the whole list.
	Status       *IndexerStatusView `json:"status,omitempty"`
	SessionState string             `json:"session_state,omitempty"`
}

// IndexerService owns indexer CRUD, testing, sessions and health bookkeeping.
type IndexerService struct {
	repo  repository.SearchRepository
	box   *internalcrypto.SecretBox
	defs  *DefinitionService
	cfg   *config.Config
	http  *httpclient.Executor
	sites *siteState

	langMu    sync.Mutex
	langCache map[string]string
}

// NewIndexerService wires the service.
func NewIndexerService(repo repository.SearchRepository, box *internalcrypto.SecretBox, defs *DefinitionService, cfg *config.Config, exec *httpclient.Executor) *IndexerService {
	return &IndexerService{repo: repo, box: box, defs: defs, cfg: cfg, http: exec, sites: newSiteState(), langCache: map[string]string{}}
}

func (s *IndexerService) siteTimeout() time.Duration {
	if s.cfg != nil && s.cfg.Search.SiteTimeoutMS > 0 {
		return time.Duration(s.cfg.Search.SiteTimeoutMS) * time.Millisecond
	}
	return 8 * time.Second
}

func (s *IndexerService) defaultInterval() int {
	if s.cfg != nil && s.cfg.Search.DefaultIntervalMS > 0 {
		return s.cfg.Search.DefaultIntervalMS
	}
	return 2000
}

// List returns every indexer with a masked view.
func (s *IndexerService) List() ([]IndexerView, error) {
	list, err := s.repo.ListIndexers()
	if err != nil {
		return nil, err
	}
	out := make([]IndexerView, 0, len(list))
	for _, idx := range list {
		out = append(out, s.view(idx))
	}
	return out, nil
}

// Get returns one indexer view.
func (s *IndexerService) Get(id uint) (*IndexerView, error) {
	idx, err := s.repo.GetIndexer(id)
	if err != nil {
		return nil, err
	}
	view := s.view(*idx)
	return &view, nil
}

// splitLanguages parses the stored comma-separated language list.
func splitLanguages(raw string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// joinLanguages normalises and stores the language list.
func joinLanguages(langs []string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(langs))
	for _, lang := range langs {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if lang == "" || seen[lang] {
			continue
		}
		seen[lang] = true
		out = append(out, lang)
	}
	return strings.Join(out, ",")
}

func (s *IndexerService) view(idx entity.SearchIndexer) IndexerView {
	apiKey := ""
	if plain, err := s.box.Decrypt(idx.APIKeyEnc); err == nil {
		apiKey = plain
	}
	secrets := s.decryptSecrets(idx.SecretsEnc)
	secretsMask := map[string]string{}
	for k, v := range secrets {
		secretsMask[k] = internalcrypto.Mask(v)
	}
	proxy := ""
	if plain, err := s.box.Decrypt(idx.ProxyURLEnc); err == nil {
		proxy = maskProxy(plain)
	}

	cookieNames := connector.CookieNames(mustDecrypt(s.box, idx.ExtraCookiesEnc))
	cookieMask := ""
	if len(cookieNames) > 0 {
		cookieMask = "已配置 " + strings.Join(cookieNames, "、")
	}

	view := IndexerView{
		SearchIndexer:    idx,
		APIKeyMask:       internalcrypto.Mask(apiKey),
		SecretsMask:      secretsMask,
		ProxyMask:        proxy,
		CookieNames:      cookieNames,
		CookieMask:       cookieMask,
		SessionState:     "none",
		DefinitionStatus: "ok",
		SearchLanguages:  splitLanguages(idx.SearchLanguages),
	}

	if status, err := s.repo.GetStatus(idx.ID); err == nil {
		remaining := int64(0)
		if status.CooldownUntil.After(time.Now()) {
			remaining = time.Until(status.CooldownUntil).Milliseconds()
		}
		view.Status = &IndexerStatusView{
			LastSuccessAt:       formatTime(status.LastSuccessAt),
			ConsecutiveFailures: status.ConsecutiveFailures,
			CooldownUntil:       formatTime(status.CooldownUntil),
			CooldownRemainingMS: remaining,
			AvgElapsedMS:        status.AvgElapsedMS,
			LastErrorKind:       status.LastErrorKind,
		}
	}
	if session, err := s.repo.GetSession(idx.ID); err == nil {
		view.SessionState = session.State
	}
	if idx.Connector == "cardigann" {
		if def, err := s.repo.GetDefinition(idx.DefinitionID); err != nil {
			view.DefinitionStatus = "missing"
		} else {
			view.SiteURL = s.defs.SiteURLOf(*def)
		}
	} else if idx.Connector == "torznab" {
		view.SiteURL = originOf(idx.BaseURL)
	}
	return view
}

// originOf trims an API base URL to its site origin, which is what a user
// expects when clicking the site name.
func originOf(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// Create validates and stores a new indexer.
func (s *IndexerService) Create(input IndexerInput) (*IndexerView, error) {
	idx, err := s.build(&entity.SearchIndexer{}, input, true)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.GetIndexerByName(idx.Name); err == nil {
		return nil, &InputError{Field: "name", Message: "名称已存在"}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := s.repo.CreateIndexer(idx); err != nil {
		return nil, &StorageError{Err: err}
	}
	_ = s.repo.SaveStatus(&entity.SearchIndexerStatus{IndexerID: idx.ID})
	view := s.view(*idx)
	return &view, nil
}

// Update applies a partial update, honouring the mask sentinel for secrets.
func (s *IndexerService) Update(id uint, input IndexerInput) (*IndexerView, error) {
	current, err := s.repo.GetIndexer(id)
	if err != nil {
		return nil, err
	}
	updated, err := s.build(current, input, false)
	if err != nil {
		return nil, err
	}
	if other, err := s.repo.GetIndexerByName(updated.Name); err == nil && other.ID != id {
		return nil, &InputError{Field: "name", Message: "名称已存在"}
	}
	if err := s.repo.UpdateIndexer(updated); err != nil {
		return nil, &StorageError{Err: err}
	}
	// A settings change invalidates the cached connector and session.
	s.sites.forget(id)
	_ = s.repo.DeleteSession(id)
	view := s.view(*updated)
	return &view, nil
}

// Delete removes an indexer and its dependent rows, keeping records (BR-31).
func (s *IndexerService) Delete(id uint) error {
	if err := s.repo.DeleteIndexer(id); err != nil {
		return err
	}
	s.sites.forget(id)
	return nil
}

// SetEnabled toggles an indexer.
func (s *IndexerService) SetEnabled(id uint, enabled bool) (*IndexerView, error) {
	idx, err := s.repo.GetIndexer(id)
	if err != nil {
		return nil, err
	}
	idx.Enabled = enabled
	if err := s.repo.UpdateIndexer(idx); err != nil {
		return nil, err
	}
	view := s.view(*idx)
	return &view, nil
}

func (s *IndexerService) build(base *entity.SearchIndexer, input IndexerInput, creating bool) (*entity.SearchIndexer, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, &InputError{Field: "name", Message: "名称不能为空"}
	}
	kind := strings.TrimSpace(input.Connector)
	if kind == "" {
		kind = "cardigann"
	}
	if !isSupportedConnector(kind) {
		return nil, &InputError{Field: "connector", Message: "只支持 cardigann / torznab / knaben / subsplease"}
	}

	out := *base
	out.Name = name
	out.Connector = kind

	switch kind {
	case "torznab":
		baseURL := strings.TrimSpace(input.BaseURL)
		if baseURL == "" {
			return nil, &InputError{Field: "base_url", Message: "接口地址不能为空"}
		}
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			return nil, &InputError{Field: "base_url", Message: "只接受 http/https 地址"}
		}
		out.BaseURL = strings.TrimRight(baseURL, "/")
		out.DefinitionID = ""
		out.DefinitionYAML = ""
		out.DefinitionVersion = ""
		enc, err := s.encryptResolved(input.APIKey, base.APIKeyEnc)
		if err != nil {
			return nil, err
		}
		out.APIKeyEnc = enc
	case "knaben", "subsplease":
		// Native connectors talk to a fixed API; only an optional host override
		// is accepted so a user can point at a mirror.
		baseURL := strings.TrimSpace(input.BaseURL)
		if baseURL != "" {
			if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
				return nil, &InputError{Field: "base_url", Message: "只接受 http/https 地址"}
			}
			out.BaseURL = strings.TrimRight(baseURL, "/")
		}
		out.DefinitionID = ""
		out.DefinitionYAML = ""
		out.DefinitionVersion = ""
		enc, err := s.encryptResolved(input.APIKey, base.APIKeyEnc)
		if err != nil {
			return nil, err
		}
		out.APIKeyEnc = enc
	case "cardigann":
		defID := strings.TrimSpace(input.DefinitionID)
		if defID == "" {
			return nil, &InputError{Field: "definition_id", Message: "请选择站点定义"}
		}
		raw, version, err := s.defs.RawDefinition(defID)
		if err != nil {
			return nil, &InputError{Field: "definition_id", Message: "定义不存在或已失效"}
		}
		result, err := cardigann.Parse(defID, []byte(raw))
		if err != nil {
			return nil, &InputError{Field: "definition_id", Message: "定义无法解析：" + err.Error()}
		}
		if result.Compat == "invalid" {
			return nil, &IncompatibleError{Field: "definition_id", Message: "定义不可用：" + strings.Join(result.Detail, "；")}
		}
		out.DefinitionID = defID
		out.DefinitionVersion = version
		out.DefinitionYAML = raw
		out.BaseURL = ""
		out.APIKeyEnc = ""
	}

	settingsJSON := ""
	if len(input.Settings) > 0 {
		raw, err := json.Marshal(input.Settings)
		if err != nil {
			return nil, err
		}
		settingsJSON = string(raw)
	}
	out.SettingsJSON = settingsJSON

	secrets := input.Secrets
	if !creating && secrets == nil {
		secrets = nil // keep existing
	}
	if secrets != nil {
		prev := s.decryptSecrets(base.SecretsEnc)
		for key, value := range secrets {
			// An empty entry keeps the stored secret, matching the button flow.
			prev[key] = internalcrypto.ResolveForUpdate(value, prev[key])
		}
		enc, err := s.encryptSecrets(prev)
		if err != nil {
			return nil, err
		}
		out.SecretsEnc = enc
	}

	out.Categories = strings.TrimSpace(input.Categories)

	// The form never prefills cookies; an empty submit means "keep the stored
	// value". The extension always sends a concrete value.
	cookies := internalcrypto.ResolveForUpdate(input.ExtraCookies, mustDecrypt(s.box, base.ExtraCookiesEnc))
	cookies = connector.NormaliseCookies(cookies)
	encCookies, err := s.box.Encrypt(cookies)
	if err != nil {
		return nil, err
	}
	out.ExtraCookiesEnc = encCookies
	out.UserAgent = strings.TrimSpace(input.UserAgent)
	if out.UserAgent != "" && len(out.UserAgent) > 512 {
		return nil, &InputError{Field: "user_agent", Message: "User-Agent 过长"}
	}

	proxy := internalcrypto.ResolveForUpdate(input.ProxyURL, mustDecrypt(s.box, base.ProxyURLEnc))
	if proxy != "" {
		if _, err := httpclient.ParseProxy(proxy); err != nil {
			return nil, &InputError{Field: "proxy_url", Message: err.Error()}
		}
	}
	encProxy, err := s.box.Encrypt(proxy)
	if err != nil {
		return nil, err
	}
	out.ProxyURLEnc = encProxy

	out.SearchLanguages = joinLanguages(input.SearchLanguages)
	if len(splitLanguages(out.SearchLanguages)) > 4 {
		return nil, &InputError{Field: "search_languages", Message: "搜索语言最多选择 4 种"}
	}

	out.MinIntervalMS = input.MinIntervalMS
	if out.MinIntervalMS <= 0 {
		out.MinIntervalMS = s.defaultInterval()
	}
	if out.MinIntervalMS < 200 {
		return nil, &InputError{Field: "min_interval_ms", Message: "请求间隔至少 200ms"}
	}
	if out.MinIntervalMS > 60000 {
		return nil, &InputError{Field: "min_interval_ms", Message: "请求间隔最多 60000ms"}
	}

	out.Priority = input.Priority
	if out.Priority <= 0 {
		out.Priority = 50
	}
	if out.Priority > 100 {
		return nil, &InputError{Field: "priority", Message: "优先级范围 1–100"}
	}

	out.DownloadClientID = input.DownloadClientID

	if input.Enabled != nil {
		out.Enabled = *input.Enabled
	} else if creating {
		out.Enabled = true
	}
	return &out, nil
}

func (s *IndexerService) encryptResolved(submitted, storedEnc string) (string, error) {
	// Blank keeps the stored value; only the explicit sentinel clears it.
	plain := internalcrypto.ResolveForUpdate(submitted, mustDecrypt(s.box, storedEnc))
	return s.box.Encrypt(plain)
}

func (s *IndexerService) encryptSecrets(secrets map[string]string) (string, error) {
	if len(secrets) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(secrets)
	if err != nil {
		return "", err
	}
	return s.box.Encrypt(string(raw))
}

func (s *IndexerService) decryptSecrets(enc string) map[string]string {
	out := map[string]string{}
	if enc == "" {
		return out
	}
	plain, err := s.box.Decrypt(enc)
	if err != nil {
		return out
	}
	_ = json.Unmarshal([]byte(plain), &out)
	return out
}

// isSupportedConnector reports whether a connector kind is implemented.
func isSupportedConnector(kind string) bool {
	switch kind {
	case "cardigann", "torznab", "knaben", "subsplease":
		return true
	}
	return false
}

// Connector builds (and caches) the connector for one indexer.
// Languages maps each indexer to the language its site publishes in. Cardigann
// sites take it from the definition; Torznab and other connectors have no
// declaration and get an empty value, which the search pipeline resolves to a
// latin title.
func (s *IndexerService) Languages(indexers []entity.SearchIndexer) map[uint]string {
	out := make(map[uint]string, len(indexers))
	for _, idx := range indexers {
		out[idx.ID] = s.DefinitionLanguage(idx)
	}
	return out
}

// DefinitionLanguage returns the definition's declared language for an indexer,
// reading the instance snapshot first so a deleted definition never changes it.
func (s *IndexerService) DefinitionLanguage(idx entity.SearchIndexer) string {
	cacheKey := idx.DefinitionID + "@" + idx.DefinitionVersion
	s.langMu.Lock()
	if lang, ok := s.langCache[cacheKey]; ok {
		s.langMu.Unlock()
		return lang
	}
	s.langMu.Unlock()

	lang := ""
	yamlText := idx.DefinitionYAML
	if yamlText == "" && idx.DefinitionID != "" && s.defs != nil {
		if raw, _, err := s.defs.RawDefinition(idx.DefinitionID); err == nil {
			yamlText = raw
		}
	}
	if yamlText != "" {
		if parsed, err := cardigann.Parse(idx.DefinitionID+".yml", []byte(yamlText)); err == nil {
			lang = parsed.Definition.Language
		}
	}

	s.langMu.Lock()
	s.langCache[cacheKey] = lang
	s.langMu.Unlock()
	return lang
}

func (s *IndexerService) Connector(ctx context.Context, id uint) (connector.Connector, error) {
	idx, err := s.repo.GetIndexer(id)
	if err != nil {
		return nil, err
	}
	if !idx.Enabled {
		return nil, &InputError{Field: "enabled", Message: "站点已停用"}
	}
	if cached := s.sites.get(id); cached != nil {
		return cached, nil
	}
	built, err := s.buildConnector(*idx)
	if err != nil {
		return nil, err
	}
	s.sites.put(id, built)
	return built, nil
}

func (s *IndexerService) buildConnector(idx entity.SearchIndexer) (connector.Connector, error) {
	proxy := mustDecrypt(s.box, idx.ProxyURLEnc)
	session := connector.Session{
		Cookies:   mustDecrypt(s.box, idx.ExtraCookiesEnc),
		UserAgent: idx.UserAgent,
	}
	switch idx.Connector {
	case "torznab":
		apiKey := mustDecrypt(s.box, idx.APIKeyEnc)
		conn := connector.NewTorznab(idx.Name, idx.BaseURL, apiKey, proxy, s.http, idx.MinIntervalMS)
		conn.SetSession(session)
		return conn, nil
	case "cardigann":
		yamlText := idx.DefinitionYAML
		if yamlText == "" {
			raw, _, err := s.defs.RawDefinition(idx.DefinitionID)
			if err != nil {
				return nil, &InputError{Field: "definition_id", Message: "定义缺失，站点进入只读状态"}
			}
			yamlText = raw
		}
		parsed, err := cardigann.Parse(idx.DefinitionID, []byte(yamlText))
		if err != nil {
			return nil, &InputError{Field: "definition_id", Message: "定义无法解析：" + err.Error()}
		}

		settings := map[string]string{}
		if idx.SettingsJSON != "" {
			_ = json.Unmarshal([]byte(idx.SettingsJSON), &settings)
		}
		secrets := s.decryptSecrets(idx.SecretsEnc)
		store := &sessionStore{repo: s.repo, box: s.box, indexerID: idx.ID}
		conn, err := connector.NewCardigann(&parsed.Definition, idx.Name, settings, secrets, store, s.http, proxy, idx.MinIntervalMS)
		if err != nil {
			return nil, err
		}
		conn.SetSession(session)
		return conn, nil
	case "knaben":
		conn := connector.NewKnaben(idx.Name, idx.BaseURL, proxy, s.http, idx.MinIntervalMS)
		conn.SetSession(session)
		return conn, nil
	case "subsplease":
		conn := connector.NewSubsPlease(idx.Name, idx.BaseURL, proxy, s.http, idx.MinIntervalMS)
		conn.SetSession(session)
		return conn, nil
	default:
		return nil, &InputError{Field: "connector", Message: "未知的连接方式"}
	}
}

// Test checks connectivity and updates the health record.
// BulkTestItem is one site's outcome of a "test all" run.
type BulkTestItem struct {
	IndexerID        uint               `json:"indexer_id"`
	OK               bool               `json:"ok"`
	Message          string             `json:"message,omitempty"`
	ErrorKind        string             `json:"error_kind,omitempty"`
	ElapsedMS        int64              `json:"elapsed_ms"`
	NeedsCaptcha     bool               `json:"needs_captcha"`
	Status           *IndexerStatusView `json:"status,omitempty"`
	SessionState     string             `json:"session_state,omitempty"`
	DefinitionStatus string             `json:"definition_status,omitempty"`
}

// TestAll tests every enabled indexer with bounded concurrency and returns one
// fresh row payload per site, so the UI can patch its list in place instead of
// reloading it. Disabled sites are skipped rather than counted as failures.
func (s *IndexerService) TestAll(ctx context.Context) []BulkTestItem {
	list, err := s.repo.ListIndexers()
	if err != nil {
		return nil
	}

	items := make([]BulkTestItem, len(list))
	const workers = 4
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				items[i] = s.testOne(ctx, list[i])
			}
		}()
	}
	for i := range list {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return items
}

func (s *IndexerService) testOne(ctx context.Context, idx entity.SearchIndexer) BulkTestItem {
	item := BulkTestItem{IndexerID: idx.ID}
	if !idx.Enabled {
		item.Message = "站点已停用，已跳过"
		return item
	}
	// A cooled-down site just failed repeatedly; testing it again would only
	// deepen the cooldown. Users can still force a single test from its row.
	if status, err := s.repo.GetStatus(idx.ID); err == nil && status.CooldownUntil.After(time.Now()) {
		view := s.view(idx)
		item.Message = "冷却中，已跳过"
		item.Status = view.Status
		item.SessionState = view.SessionState
		item.DefinitionStatus = view.DefinitionStatus
		return item
	}

	result, err := s.Test(ctx, idx.ID)
	if err != nil {
		item.Message = err.Error()
	}
	if result != nil {
		item.OK = result.OK
		item.Message = result.Message
		item.ErrorKind = result.ErrorKind
		item.ElapsedMS = result.ElapsedMS
		item.NeedsCaptcha = result.NeedsCaptcha
		item.Status = result.Status
		item.SessionState = result.SessionState
	}
	// DefinitionStatus can change between tests (definition deleted), so read
	// it fresh for the row.
	if fresh, err := s.repo.GetIndexer(idx.ID); err == nil {
		view := s.view(*fresh)
		item.DefinitionStatus = view.DefinitionStatus
		if item.Status == nil {
			item.Status = view.Status
		}
		if item.SessionState == "" {
			item.SessionState = view.SessionState
		}
	}
	return item
}

// Test runs a connectivity test (API-09).
func (s *IndexerService) Test(ctx context.Context, id uint) (*TestResult, error) {
	idx, err := s.repo.GetIndexer(id)
	if err != nil {
		return nil, err
	}
	conn, err := s.Connector(ctx, id)
	if err != nil {
		return &TestResult{OK: false, ErrorKind: "unsupported", Message: err.Error()}, nil
	}

	testCtx, cancel := context.WithTimeout(ctx, s.siteTimeout())
	defer cancel()
	start := time.Now()
	caps, err := safeConnectorTest(conn, testCtx)
	elapsed := time.Since(start)

	if err != nil {
		kind := failureKind(err)
		field := ""
		var cErr *connector.Error
		if errors.As(err, &cErr) {
			field = cErr.Field
		}
		s.RecordFailure(id, kind)
		result := &TestResult{
			OK: false, IndexerName: idx.Name, ErrorKind: kind,
			Message: failureMessage(err), Field: field,
			ElapsedMS: elapsed.Milliseconds(),
		}
		s.attachRowState(id, result)
		return result, nil
	}

	s.RecordSuccess(id, int(elapsed.Milliseconds()))
	result := &TestResult{
		OK:           true,
		IndexerName:  caps.Name,
		Protocol:     caps.Protocol,
		Categories:   caps.Categories,
		SearchModes:  caps.SearchModes,
		ElapsedMS:    elapsed.Milliseconds(),
		LoginState:   caps.LoginState,
		NeedsCaptcha: caps.NeedsCaptcha,
	}
	s.attachRowState(id, result)
	return result, nil
}

// safeConnectorTest is the probing equivalent of safeSiteSearch: one broken
// definition reports a parse failure instead of crashing the test run.
func safeConnectorTest(conn connector.Connector, ctx context.Context) (caps *connector.Capabilities, err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("search: 站点测试时崩溃: %v", r)
			caps = nil
			err = &connector.Error{
				Kind:    connector.KindParse,
				Message: fmt.Sprintf("该站点定义处理失败（%v）", r),
			}
		}
	}()
	return conn.Test(ctx)
}

// attachRowState gives the UI the fresh health/session data for one row so a
// test does not need a follow-up list request.
func (s *IndexerService) attachRowState(id uint, result *TestResult) {
	if idx, err := s.repo.GetIndexer(id); err == nil {
		view := s.view(*idx)
		result.Status = view.Status
		result.SessionState = view.SessionState
	}
}

// SubmitCaptcha completes login for a captcha-protected definition.
func (s *IndexerService) SubmitCaptcha(ctx context.Context, id uint, captcha string) (*TestResult, error) {
	conn, err := s.Connector(ctx, id)
	if err != nil {
		return nil, err
	}
	type captchaLogin interface {
		LoginWithCaptcha(ctx context.Context, captcha string) error
	}
	login, ok := conn.(captchaLogin)
	if !ok {
		return &TestResult{OK: false, ErrorKind: "unsupported", Message: "该站点不需要验证码"}, nil
	}
	if err := login.LoginWithCaptcha(ctx, captcha); err != nil {
		return &TestResult{OK: false, ErrorKind: failureKind(err), Message: failureMessage(err)}, nil
	}
	// Persist the session state so the UI can show "已登录".
	session, err := s.repo.GetSession(id)
	if err == nil {
		session.State = "logged-in"
		_ = s.repo.SaveSession(session)
	}
	return &TestResult{OK: true, IndexerName: (func() string {
		i, _ := s.repo.GetIndexer(id)
		if i != nil {
			return i.Name
		}
		return ""
	})(), LoginState: "已登录"}, nil
}

// RefreshDefinition switches an instance to the latest definition snapshot.
func (s *IndexerService) RefreshDefinition(id uint) (*IndexerView, error) {
	idx, err := s.repo.GetIndexer(id)
	if err != nil {
		return nil, err
	}
	if idx.Connector != "cardigann" {
		return nil, &InputError{Field: "connector", Message: "只有 Cardigann 站点需要刷新定义"}
	}
	raw, version, err := s.defs.RawDefinition(idx.DefinitionID)
	if err != nil {
		return nil, &InputError{Field: "definition_id", Message: "定义缺失"}
	}
	result, err := cardigann.Parse(idx.DefinitionID, []byte(raw))
	if err != nil || result.Compat == "invalid" {
		return nil, &IncompatibleError{Field: "definition_id", Message: "新定义不可用"}
	}
	idx.DefinitionYAML = raw
	idx.DefinitionVersion = version
	if err := s.repo.UpdateIndexer(idx); err != nil {
		return nil, err
	}
	s.sites.forget(id)
	view := s.view(*idx)
	return &view, nil
}

// RecordSuccess updates health after a successful call (BR-06).
func (s *IndexerService) RecordSuccess(id uint, elapsedMS int) {
	status, err := s.repo.GetStatus(id)
	if err != nil {
		status = &entity.SearchIndexerStatus{IndexerID: id}
	}
	status.ConsecutiveFailures = 0
	status.CooldownUntil = time.Time{}
	status.LastErrorKind = ""
	status.LastSuccessAt = time.Now()
	if elapsedMS > 0 {
		if status.SampleCount < 20 {
			status.SampleCount++
		}
		// Running average over the last 20 samples.
		status.AvgElapsedMS = (status.AvgElapsedMS*(status.SampleCount-1) + elapsedMS) / status.SampleCount
	}
	_ = s.repo.SaveStatus(status)
}

// RecordFailure counts a failure and starts a 15-minute cooldown after three
// consecutive failures.
func (s *IndexerService) RecordFailure(id uint, kind string) {
	status, err := s.repo.GetStatus(id)
	if err != nil {
		status = &entity.SearchIndexerStatus{IndexerID: id}
	}
	status.ConsecutiveFailures++
	status.LastErrorKind = kind
	if status.ConsecutiveFailures >= 3 {
		status.CooldownUntil = time.Now().Add(15 * time.Minute)
	}
	_ = s.repo.SaveStatus(status)
}

// ClearCooldown lets the user resume a cooled-down site early.
func (s *IndexerService) ClearCooldown(id uint) error {
	status, err := s.repo.GetStatus(id)
	if err != nil {
		return err
	}
	status.CooldownUntil = time.Time{}
	_ = s.repo.SaveStatus(status)
	return nil
}

// Statuses returns the health panel payload.
// LanguageFacets counts enabled indexers per site language, so the search page
// can offer a multi-select filter. Each entry reports the raw tag (what the
// search accepts), a Chinese label, and how many sites it covers.
type LanguageFacet struct {
	Language string `json:"language"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
}

// LanguageFacets returns the language distribution of enabled sites, busiest
// first. Sites whose definition declares no language are grouped under "", which
// the UI shows as "未声明语言".
func (s *IndexerService) LanguageFacets() ([]LanguageFacet, error) {
	list, err := s.repo.ListIndexers()
	if err != nil {
		return nil, err
	}

	type bucket struct {
		count int
		label string
	}
	byGroup := map[string]*bucket{}
	for _, idx := range list {
		if !idx.Enabled {
			continue
		}
		// Count the languages the site is actually searched in. A site whose
		// definition says en-US but is overridden to zh+en should show up under
		// both, otherwise the filter offers a language that matches nothing.
		langs := splitLanguages(idx.SearchLanguages)
		if len(langs) == 0 {
			langs = []string{s.DefinitionLanguage(idx)}
		}
		for _, tag := range langs {
			group := languageGroup(tag)
			b := byGroup[group]
			if b == nil {
				b = &bucket{label: LanguageLabel(tag)}
				byGroup[group] = b
			}
			b.count++
		}
	}

	out := make([]LanguageFacet, 0, len(byGroup))
	for group, b := range byGroup {
		out = append(out, LanguageFacet{Language: group, Label: b.label, Count: b.count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}

func (s *IndexerService) Statuses() ([]IndexerStatusView, error) {
	list, err := s.repo.ListIndexers()
	if err != nil {
		return nil, err
	}
	out := make([]IndexerStatusView, 0, len(list))
	for _, idx := range list {
		status, err := s.repo.GetStatus(idx.ID)
		if err != nil {
			out = append(out, IndexerStatusView{})
			continue
		}
		remaining := int64(0)
		if status.CooldownUntil.After(time.Now()) {
			remaining = time.Until(status.CooldownUntil).Milliseconds()
		}
		out = append(out, IndexerStatusView{
			LastSuccessAt:       formatTime(status.LastSuccessAt),
			ConsecutiveFailures: status.ConsecutiveFailures,
			CooldownUntil:       formatTime(status.CooldownUntil),
			CooldownRemainingMS: remaining,
			AvgElapsedMS:        status.AvgElapsedMS,
			LastErrorKind:       status.LastErrorKind,
		})
	}
	return out, nil
}

// InputError is a validation failure tied to one field.
type InputError struct {
	Field   string
	Message string
}

func (e *InputError) Error() string { return e.Message }

// IncompatibleError marks a definition that cannot be instantiated (40301).
type IncompatibleError struct {
	Field   string
	Message string
}

func (e *IncompatibleError) Error() string { return e.Message }

// StorageError wraps a persistence failure so handlers can answer 50001.
type StorageError struct{ Err error }

func (e *StorageError) Error() string { return "保存失败：" + e.Err.Error() }
func (e *StorageError) Unwrap() error { return e.Err }

func mustDecrypt(box *internalcrypto.SecretBox, enc string) string {
	if box == nil || enc == "" {
		return ""
	}
	plain, err := box.Decrypt(enc)
	if err != nil {
		return ""
	}
	return plain
}

func maskProxy(raw string) string {
	if raw == "" {
		return ""
	}
	// Keep the scheme and host, mask the credentials.
	at := strings.LastIndex(raw, "@")
	if at < 0 {
		return raw
	}
	schemeEnd := strings.Index(raw, "://")
	if schemeEnd < 0 {
		return "****" + raw[at:]
	}
	return raw[:schemeEnd+3] + "****@" + raw[at+1:]
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func formatDuration(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return fmt.Sprintf("%ds", ms/1000)
}

// sessionStore adapts the repository to connector.SessionStore.
type sessionStore struct {
	repo      repository.SearchRepository
	box       *internalcrypto.SecretBox
	indexerID uint
}

func (s *sessionStore) Load(ctx context.Context) (map[string]string, string, error) {
	session, err := s.repo.GetSession(s.indexerID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "none", nil
		}
		return nil, "", err
	}
	out := map[string]string{}
	if session.CookieEnc != "" {
		plain, decErr := s.box.Decrypt(session.CookieEnc)
		if decErr == nil {
			_ = json.Unmarshal([]byte(plain), &out)
		}
	}
	return out, session.State, nil
}

func (s *sessionStore) Save(ctx context.Context, cookies map[string]string, expiresAt time.Time) error {
	raw, err := json.Marshal(cookies)
	if err != nil {
		return err
	}
	enc, err := s.box.Encrypt(string(raw))
	if err != nil {
		return err
	}
	return s.repo.SaveSession(&entity.SearchIndexerSession{
		IndexerID: s.indexerID, State: "logged-in", CookieEnc: enc, ExpiresAt: expiresAt,
	})
}

func (s *sessionStore) MarkFailed(ctx context.Context, reason string) error {
	return s.repo.SaveSession(&entity.SearchIndexerSession{
		IndexerID: s.indexerID, State: "failed",
	})
}

func (s *sessionStore) MarkPending(ctx context.Context) error {
	return s.repo.SaveSession(&entity.SearchIndexerSession{
		IndexerID: s.indexerID, State: "pending",
	})
}

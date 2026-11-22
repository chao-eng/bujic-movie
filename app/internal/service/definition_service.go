package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/config"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/service/cardigann"
	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"gorm.io/gorm"
)

// DefinitionView is the read model for the definition library.
type DefinitionView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Source       string   `json:"source"`
	Version      string   `json:"version"`
	Type         string   `json:"type"`
	Language     string   `json:"language"`
	Compat       string   `json:"compat"`
	Detail       []string `json:"detail"`
	Capabilities string   `json:"capabilities"`
	UpdatedAt    string   `json:"updated_at"`
	// NeedsLogin/NeedsCaptcha drive the "add" flow: a template that needs a
	// login should send the user straight to the credential form.
	NeedsLogin   bool `json:"needs_login"`
	NeedsCaptcha bool `json:"needs_captcha"`
	// LanguageLabel is a human name for the definition's language so the list
	// can show "简体中文" instead of "zh-CN".
	LanguageLabel string `json:"language_label"`
	// CategoryGroups are the Newznab top-level groups this template can serve,
	// as Chinese labels. Used for category filtering in the template browser.
	CategoryGroups []string `json:"category_groups"`
	// ModeSummary lists the structured search modes in plain words.
	ModeSummary string `json:"mode_summary"`
	// SiteURL is the definition's primary homepage, so the browser can open the
	// site the template configures.
	SiteURL string `json:"site_url"`
	// UsedCount is how many sites already use this template.
	UsedCount int `json:"used_count"`
}

// SyncSummary reports what a sync or import changed.
type SyncSummary struct {
	Added        int              `json:"added"`
	Updated      int              `json:"updated"`
	Unchanged    int              `json:"unchanged"`
	Invalid      int              `json:"invalid"`
	Conflict     int              `json:"conflict"`
	Total        int              `json:"total"`
	InvalidItems []DefinitionView `json:"invalid_items,omitempty"`
	Message      string           `json:"message,omitempty"`
}

// SourceTestResult reports whether the definition source is reachable through
// the current proxy settings. It is what the settings page uses to test a
// proxy: it must stay cheap, so it fetches only the definition list.
type SourceTestResult struct {
	OK        bool   `json:"ok"`
	URL       string `json:"url"`
	Count     int    `json:"count"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Message   string `json:"message"`
}

// DefinitionService owns the definition library: upstream sync, local import,
// validation and the raw YAML used to instantiate indexers.
type DefinitionService struct {
	repo   repository.SearchRepository
	cfg    *config.Config
	http   *httpclient.Executor
	syncMu sync.Mutex

	siteMu    sync.Mutex
	siteCache map[string]string
}

// NewDefinitionService wires the service.
func NewDefinitionService(repo repository.SearchRepository, cfg *config.Config, exec *httpclient.Executor) *DefinitionService {
	return &DefinitionService{repo: repo, cfg: cfg, http: exec, siteCache: map[string]string{}}
}

// SiteURLOf returns the definition's homepage without re-parsing its YAML for
// every indexer in a list. The cache is keyed by definition id + version, so a
// sync that updates a definition naturally invalidates it.
func (s *DefinitionService) SiteURLOf(def entity.SearchIndexerDefinition) string {
	key := def.ID + "@" + def.Version

	s.siteMu.Lock()
	if cached, ok := s.siteCache[key]; ok {
		s.siteMu.Unlock()
		return cached
	}
	s.siteMu.Unlock()

	url := ""
	if parsed, err := cardigann.Parse(def.ID+".yml", []byte(def.RawYAML)); err == nil && len(parsed.Definition.Links) > 0 {
		url = parsed.Definition.Links[0]
	}

	s.siteMu.Lock()
	s.siteCache[key] = url
	s.siteMu.Unlock()
	return url
}

// LanguageLabel renders a language code as a short Chinese name.
//
// Definitions store full BCP-47 tags ("ru-RU", "pt-BR"), so the lookup first
// tries the exact tag in the explicit table, then the bare language subtag. A
// bare subtag alone is not enough: before this, "ru-RU" fell through to the
// fallback and the site filter listed raw codes like "RU", "DE", "HU".
func LanguageLabel(code string) string {
	normalised := strings.ToLower(strings.TrimSpace(code))
	if normalised == "" {
		return "未知"
	}

	// Primary language subtag: "ru-ru" -> "ru", "zh-hant-tw" -> "zh".
	primary := normalised
	if idx := strings.Index(normalised, "-"); idx > 0 {
		primary = normalised[:idx]
	}

	// Script/region variants that need their own name.
	switch normalised {
	case "zh-cn", "zh-hans", "zh-sg", "zh-my":
		return "简体中文"
	case "zh-tw", "zh-hant", "zh-hk", "zh-mo":
		return "繁体中文"
	}

	if name, ok := languageNames[primary]; ok {
		return name
	}
	// Unknown but well-formed tag: show the language subtag upper-cased rather
	// than the whole tag ("xx-YY" always reads as "XX").
	return strings.ToUpper(primary)
}

// languageNames maps an ISO-639-1 language subtag to its Chinese name. It covers
// every language present in the upstream Cardigann library plus the common ones
// definitions are likely to add later.
var languageNames = map[string]string{
	"af": "南非荷兰语",
	"ar": "阿拉伯语",
	"az": "阿塞拜疆语",
	"be": "白俄罗斯语",
	"bg": "保加利亚语",
	"bn": "孟加拉语",
	"bs": "波斯尼亚语",
	"ca": "加泰罗尼亚语",
	"cs": "捷克语",
	"cy": "威尔士语",
	"da": "丹麦语",
	"de": "德语",
	"el": "希腊语",
	"en": "英语",
	"eo": "世界语",
	"es": "西班牙语",
	"et": "爱沙尼亚语",
	"eu": "巴斯克语",
	"fa": "波斯语",
	"fi": "芬兰语",
	"fr": "法语",
	"ga": "爱尔兰语",
	"gl": "加利西亚语",
	"he": "希伯来语",
	"hi": "印地语",
	"hr": "克罗地亚语",
	"hu": "匈牙利语",
	"hy": "亚美尼亚语",
	"id": "印尼语",
	"is": "冰岛语",
	"it": "意大利语",
	"ja": "日语",
	"ka": "格鲁吉亚语",
	"kk": "哈萨克语",
	"km": "高棉语",
	"ko": "韩语",
	"ku": "库尔德语",
	"ky": "吉尔吉斯语",
	"la": "拉丁语",
	"lt": "立陶宛语",
	"lv": "拉脱维亚语",
	"mk": "马其顿语",
	"ml": "马拉雅拉姆语",
	"mn": "蒙古语",
	"ms": "马来语",
	"mt": "马耳他语",
	"nb": "挪威语",
	"ne": "尼泊尔语",
	"nl": "荷兰语",
	"nn": "挪威语（新诺斯克）",
	"no": "挪威语",
	"pa": "旁遮普语",
	"pl": "波兰语",
	"ps": "普什图语",
	"pt": "葡萄牙语",
	"ro": "罗马尼亚语",
	"ru": "俄语",
	"si": "僧伽罗语",
	"sk": "斯洛伐克语",
	"sl": "斯洛文尼亚语",
	"sq": "阿尔巴尼亚语",
	"sr": "塞尔维亚语",
	"sv": "瑞典语",
	"sw": "斯瓦希里语",
	"ta": "泰米尔语",
	"te": "泰卢固语",
	"tg": "塔吉克语",
	"th": "泰语",
	"tl": "他加禄语",
	"tr": "土耳其语",
	"uk": "乌克兰语",
	"ur": "乌尔都语",
	"uz": "乌兹别克语",
	"vi": "越南语",
	"zh": "中文",
}

// definitionShape summarises the categories and search modes a template covers,
// using labels the template browser can filter on.
func definitionShape(def *cardigann.Definition) ([]string, string) {
	groups := []string{}
	seen := map[string]bool{}
	add := func(label string) {
		if label == "" || seen[label] {
			return
		}
		seen[label] = true
		groups = append(groups, label)
	}
	for name := range def.Caps.Categories {
		add(cardigann.CategoryGroupLabel(name))
	}
	for _, cm := range def.Caps.CategoryMappings {
		add(cardigann.CategoryGroupLabel(cm.Cat))
	}

	modes := []string{}
	if len(def.Caps.Modes["search"]) > 0 {
		modes = append(modes, "关键词")
	}
	if len(def.Caps.Modes["tv-search"]) > 0 {
		modes = append(modes, "剧集")
	}
	if len(def.Caps.Modes["movie-search"]) > 0 {
		modes = append(modes, "电影")
	}
	if len(def.Caps.Modes["music-search"]) > 0 {
		modes = append(modes, "音乐")
	}
	if len(def.Caps.Modes["book-search"]) > 0 {
		modes = append(modes, "书籍")
	}
	summary := ""
	if len(modes) > 0 {
		summary = "支持 " + strings.Join(modes, "、") + " 搜索"
	}
	return groups, summary
}

// List returns the definition library, annotated with how many sites use it.
func (s *DefinitionService) List() ([]DefinitionView, error) {
	defs, err := s.repo.ListDefinitions()
	if err != nil {
		return nil, err
	}
	used := map[string]int{}
	if indexers, err := s.repo.ListIndexers(); err == nil {
		for _, idx := range indexers {
			if idx.Connector == "cardigann" && idx.DefinitionID != "" {
				used[idx.DefinitionID]++
			}
		}
	}
	out := make([]DefinitionView, 0, len(defs))
	for _, def := range defs {
		view := toDefinitionView(def)
		view.UsedCount = used[def.ID]
		out = append(out, view)
	}
	return out, nil
}

// Get returns one definition view.
func (s *DefinitionService) Get(id string) (*DefinitionView, error) {
	def, err := s.repo.GetDefinition(id)
	if err != nil {
		return nil, err
	}
	view := toDefinitionView(*def)
	return &view, nil
}

// RawDefinition returns the stored YAML plus its version, used when building a
// connector or taking an instance snapshot.
func (s *DefinitionService) RawDefinition(id string) (string, string, error) {
	def, err := s.repo.GetDefinition(id)
	if err != nil {
		return "", "", err
	}
	return def.RawYAML, def.Version, nil
}

func toDefinitionView(def entity.SearchIndexerDefinition) DefinitionView {
	detail := []string{}
	if def.CompatDetail != "" {
		_ = json.Unmarshal([]byte(def.CompatDetail), &detail)
	}
	view := DefinitionView{
		ID: def.ID, Name: def.Name, Source: def.Source, Version: def.Version,
		Type: def.Type, Language: def.Language, Compat: def.Compat,
		Detail: detail, Capabilities: def.CapabilitiesJSON,
		UpdatedAt:     formatTime(def.UpdatedAt),
		LanguageLabel: LanguageLabel(def.Language),
	}
	// Parse the snapshot to answer "does this template need credentials?" and
	// what it can search.
	if parsed, err := cardigann.Parse(def.ID+".yml", []byte(def.RawYAML)); err == nil {
		view.NeedsLogin = parsed.Definition.UsesLogin()
		view.NeedsCaptcha = parsed.Definition.NeedsCaptcha()
		view.CategoryGroups, view.ModeSummary = definitionShape(&parsed.Definition)
		if len(parsed.Definition.Links) > 0 {
			view.SiteURL = parsed.Definition.Links[0]
		}
	}
	return view
}

// Import parses and stores one YAML definition supplied locally.
func (s *DefinitionService) Import(fileName string, raw []byte) (*SyncSummary, error) {
	summary := &SyncSummary{Total: 1}
	res, err := cardigann.Parse(fileName, raw)
	if err != nil {
		summary.Invalid = 1
		summary.InvalidItems = append(summary.InvalidItems, DefinitionView{
			ID: fileName, Name: fileName, Compat: "invalid",
			Detail: []string{err.Error()},
		})
		summary.Message = err.Error()
		return summary, nil
	}

	if res.Compat == "invalid" {
		summary.Invalid = 1
		summary.InvalidItems = append(summary.InvalidItems, DefinitionView{
			ID: res.Definition.ID, Name: res.Definition.Name, Compat: "invalid",
			Detail: res.Detail,
		})
		summary.Message = strings.Join(res.Detail, "；")
		return summary, nil
	}

	existing, err := s.repo.GetDefinition(res.Definition.ID)
	action := "added"
	if err == nil {
		if existing.RawHash == res.Hash {
			return summary, nil
		}
		action = "updated"
	}

	detailJSON, _ := json.Marshal(res.Detail)
	def := &entity.SearchIndexerDefinition{
		ID:               res.Definition.ID,
		Name:             res.Definition.Name,
		Source:           "local",
		Version:          res.Hash[:12],
		Type:             defaultString(res.Definition.Type, "public"),
		Language:         defaultString(res.Definition.Language, "en-US"),
		CapabilitiesJSON: cardigann.CapabilitiesSummary(&res.Definition),
		Compat:           res.Compat,
		CompatDetail:     string(detailJSON),
		RawYAML:          string(raw),
		RawHash:          res.Hash,
	}
	if err := s.repo.UpsertDefinition(def); err != nil {
		return nil, &StorageError{Err: err}
	}
	if action == "added" {
		summary.Added++
	} else {
		summary.Updated++
	}
	return summary, nil
}

// definitionMeta mirrors the upstream definition index entry.
type definitionMeta struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	File     string `json:"file"`
	Version  int    `json:"version"`
	Language string `json:"language"`
	Type     string `json:"type"`
}

// TestSource fetches the definition list once through the current proxy and
// reports the outcome. It never downloads definition files.
func (s *DefinitionService) TestSource(ctx context.Context) *SourceTestResult {
	result := &SourceTestResult{}
	if s.cfg == nil || strings.TrimSpace(s.cfg.Search.DefinitionSource) == "" {
		result.Message = "未配置定义源"
		return result
	}
	source := strings.TrimRight(s.cfg.Search.DefinitionSource, "/")
	version := s.cfg.Search.DefinitionVersion
	if version == "" {
		version = "12"
	}
	listURL := fmt.Sprintf("%s/master/%s", source, version)
	result.URL = listURL

	start := time.Now()
	resp, err := s.http.Do(ctx, httpclient.Request{
		URL:      listURL,
		Timeout:  10 * time.Second,
		Allowed:  httpclient.HostsFromLinks([]string{source}),
		MaxBytes: 5 << 20,
		// A proxy check should fail fast rather than retry three times.
		NoRetry: true,
	})
	result.ElapsedMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Message = "代理或网络不可用：" + err.Error()
		return result
	}

	var entries []definitionMeta
	if err := json.Unmarshal(resp.Body, &entries); err != nil {
		var keyed map[string]definitionMeta
		if err2 := json.Unmarshal(resp.Body, &keyed); err2 != nil {
			result.Message = "定义源返回的内容不是有效清单"
			return result
		}
		result.Count = len(keyed)
	} else {
		result.Count = len(entries)
	}
	result.OK = true
	result.Message = fmt.Sprintf("连接正常，定义源返回 %d 条", result.Count)
	return result
}

// Sync pulls the upstream definition package and stores every parseable entry.
// A failure keeps the existing library untouched (BR-27).
//
// Prowlarr publishes one package.zip per version, and individual definitions at
// `/{id}` (without the .yml suffix). Downloading the package once is both faster
// and more reliable than hundreds of separate requests.
func (s *DefinitionService) Sync(ctx context.Context) (*SyncSummary, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	if s.cfg == nil || strings.TrimSpace(s.cfg.Search.DefinitionSource) == "" {
		return nil, &InputError{Field: "definition_source", Message: "未配置定义源"}
	}
	source := strings.TrimRight(s.cfg.Search.DefinitionSource, "/")
	version := s.cfg.Search.DefinitionVersion
	if version == "" {
		version = "12"
	}
	allowed := httpclient.HostsFromLinks([]string{source})

	maxDefs := 1000
	if s.cfg.Search.MaxDefinitions > 0 {
		maxDefs = s.cfg.Search.MaxDefinitions
	}

	summary := &SyncSummary{}
	zipURL := fmt.Sprintf("%s/master/%s/package.zip", source, version)
	resp, err := s.http.Do(ctx, httpclient.Request{
		URL:      zipURL,
		Timeout:  90 * time.Second,
		Allowed:  allowed,
		MaxBytes: 128 << 20,
	})
	if err != nil {
		return nil, fmt.Errorf("定义包下载失败：%w", err)
	}
	if err := s.syncFromPackage(ctx, resp.Body, version, maxDefs, summary); err != nil {
		return nil, err
	}

	if summary.Added == 0 && summary.Updated == 0 && summary.Unchanged == 0 {
		return summary, errors.New("本次同步没有可用定义")
	}
	return summary, nil
}

// syncFromPackage walks package.zip and stores each definition.
func (s *DefinitionService) syncFromPackage(ctx context.Context, raw []byte, version string, maxDefs int, summary *SyncSummary) error {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return fmt.Errorf("定义包不是有效的 zip：%w", err)
	}

	processed := 0
	for _, file := range reader.File {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := file.Name
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".yaml") {
			continue
		}
		if processed >= maxDefs {
			break
		}
		processed++

		body, readErr := readZipFile(file)
		if readErr != nil {
			summary.Invalid++
			continue
		}
		s.storeDefinition(name, body, version, summary)
	}
	return nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, cardigann.MaxDefinitionBytes))
}

// storeDefinition parses and persists one definition, updating the summary.
func (s *DefinitionService) storeDefinition(name string, raw []byte, version string, summary *SyncSummary) {
	parsed, err := cardigann.Parse(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))+".yml", raw)
	if err != nil || parsed == nil {
		summary.Invalid++
		reason := "解析失败"
		if err != nil {
			reason = err.Error()
		}
		summary.InvalidItems = append(summary.InvalidItems, DefinitionView{
			ID: name, Name: name, Compat: "invalid", Detail: []string{reason},
		})
		return
	}
	if parsed.Compat == "invalid" {
		summary.Invalid++
		summary.InvalidItems = append(summary.InvalidItems, DefinitionView{
			ID: parsed.Definition.ID, Name: parsed.Definition.Name, Compat: "invalid",
			Detail: parsed.Detail,
		})
		return
	}

	existing, getErr := s.repo.GetDefinition(parsed.Definition.ID)
	// Local imports win over upstream (BR-23).
	if getErr == nil && existing.Source == "local" {
		summary.Conflict++
		return
	}
	action := "added"
	if getErr == nil {
		if existing.RawHash == parsed.Hash {
			summary.Unchanged++
			summary.Total++
			return
		}
		action = "updated"
	}

	detailJSON, _ := json.Marshal(parsed.Detail)
	def := &entity.SearchIndexerDefinition{
		ID:               parsed.Definition.ID,
		Name:             parsed.Definition.Name,
		Source:           "remote",
		Version:          version,
		Type:             defaultString(parsed.Definition.Type, "public"),
		Language:         defaultString(parsed.Definition.Language, "en-US"),
		CapabilitiesJSON: cardigann.CapabilitiesSummary(&parsed.Definition),
		Compat:           parsed.Compat,
		CompatDetail:     string(detailJSON),
		RawYAML:          string(raw),
		RawHash:          parsed.Hash,
	}
	if err := s.repo.UpsertDefinition(def); err != nil {
		logger.Warn("search: definition upsert failed for %s: %v", parsed.Definition.ID, err)
		return
	}
	if action == "added" {
		summary.Added++
	} else {
		summary.Updated++
	}
	summary.Total++
}

// Delete removes one definition. Existing instances keep their snapshot and
// become read-only (BR-38).
func (s *DefinitionService) Delete(id string) error {
	if _, err := s.repo.GetDefinition(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return err
	}
	return s.repo.DeleteDefinition(id)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

var _ = io.EOF
var _ = http.StatusOK

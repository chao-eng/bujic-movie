package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/config"
	internalcrypto "github.com/bujic-movie/bujic-movie/internal/crypto"
	"github.com/bujic-movie/bujic-movie/internal/model/entity"
	"github.com/bujic-movie/bujic-movie/internal/repository"
	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
	"github.com/bujic-movie/bujic-movie/internal/service/qbittorrent"
	"gorm.io/gorm"
)

// DownloadClientInput is the API payload for a download client.
type DownloadClientInput struct {
	Name            string                 `json:"name"`
	Kind            string                 `json:"kind"`
	Host            string                 `json:"host"`
	Port            int                    `json:"port"`
	UseSSL          bool                   `json:"use_ssl"`
	URLBase         string                 `json:"url_base"`
	APIKey          string                 `json:"api_key"`
	Username        string                 `json:"username"`
	Password        string                 `json:"password"`
	DefaultCategory string                 `json:"default_category"`
	InitialState    string                 `json:"initial_state"`
	Sequential      bool                   `json:"sequential"`
	FirstLastFirst  bool                   `json:"first_last_first"`
	ContentLayout   string                 `json:"content_layout"`
	Priority        int                    `json:"priority"`
	IsDefault       bool                   `json:"is_default"`
	Enabled         *bool                  `json:"enabled"`
	Categories      []CategoryMappingInput `json:"categories"`
}

// CategoryMappingInput is one source→client category mapping.
type CategoryMappingInput struct {
	SourceCategory int    `json:"source_category"`
	ClientCategory string `json:"client_category"`
}

// DownloadClientView masks credentials.
type DownloadClientView struct {
	entity.SearchDownloadClient
	APIKeyMask    string                 `json:"api_key_mask"`
	PasswordMask  string                 `json:"password_mask"`
	Categories    []CategoryMappingInput `json:"category_mappings"`
	UnavailableIn int64                  `json:"unavailable_remaining_ms"`
}

// DownloadClientService owns client CRUD, category mapping, selection and
// failover. It never decides a save path: that belongs to qBittorrent (D-04).
type DownloadClientService struct {
	repo repository.SearchRepository
	box  *internalcrypto.SecretBox
	cfg  *config.Config
	http *httpclient.Executor
}

// NewDownloadClientService wires the service.
func NewDownloadClientService(repo repository.SearchRepository, box *internalcrypto.SecretBox, cfg *config.Config, exec *httpclient.Executor) *DownloadClientService {
	return &DownloadClientService{repo: repo, box: box, cfg: cfg, http: exec}
}

// List returns all clients with masked credentials.
func (s *DownloadClientService) List() ([]DownloadClientView, error) {
	list, err := s.repo.ListClients()
	if err != nil {
		return nil, err
	}
	out := make([]DownloadClientView, 0, len(list))
	for _, client := range list {
		view, err := s.view(client)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// Get returns one client.
func (s *DownloadClientService) Get(id uint) (*DownloadClientView, error) {
	client, err := s.repo.GetClient(id)
	if err != nil {
		return nil, err
	}
	view, err := s.view(*client)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *DownloadClientService) view(client entity.SearchDownloadClient) (DownloadClientView, error) {
	apiKey := mustDecrypt(s.box, client.APIKeyEnc)
	password := mustDecrypt(s.box, client.PasswordEnc)
	cats, err := s.repo.ListClientCategories(client.ID)
	if err != nil {
		return DownloadClientView{}, err
	}
	mappings := make([]CategoryMappingInput, 0, len(cats))
	for _, c := range cats {
		mappings = append(mappings, CategoryMappingInput{SourceCategory: c.SourceCategory, ClientCategory: c.ClientCategory})
	}
	remaining := int64(0)
	if client.UnavailableUntil.After(time.Now()) {
		remaining = time.Until(client.UnavailableUntil).Milliseconds()
	}
	return DownloadClientView{
		SearchDownloadClient: client,
		APIKeyMask:           internalcrypto.Mask(apiKey),
		PasswordMask:         internalcrypto.Mask(password),
		Categories:           mappings,
		UnavailableIn:        remaining,
	}, nil
}

// Create validates and stores a new client.
func (s *DownloadClientService) Create(input DownloadClientInput) (*DownloadClientView, error) {
	client, err := s.build(&entity.SearchDownloadClient{}, input, true)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.GetClientByName(client.Name); err == nil {
		return nil, &InputError{Field: "name", Message: "名称已存在"}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := s.repo.CreateClient(client); err != nil {
		return nil, &StorageError{Err: err}
	}
	if err := s.replaceMappings(client.ID, input.Categories); err != nil {
		return nil, err
	}
	view, err := s.view(*client)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

// Update applies a partial update with the mask sentinel for secrets.
func (s *DownloadClientService) Update(id uint, input DownloadClientInput) (*DownloadClientView, error) {
	current, err := s.repo.GetClient(id)
	if err != nil {
		return nil, err
	}
	updated, err := s.build(current, input, false)
	if err != nil {
		return nil, err
	}
	if other, err := s.repo.GetClientByName(updated.Name); err == nil && other.ID != id {
		return nil, &InputError{Field: "name", Message: "名称已存在"}
	}
	if err := s.repo.UpdateClient(updated); err != nil {
		return nil, &StorageError{Err: err}
	}
	if input.Categories != nil {
		if err := s.replaceMappings(id, input.Categories); err != nil {
			return nil, err
		}
	}
	view, err := s.view(*updated)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

// Delete removes a client, its mappings, and unbinds indexers.
func (s *DownloadClientService) Delete(id uint) error {
	return s.repo.DeleteClient(id)
}

// SetEnabled toggles a client.
func (s *DownloadClientService) SetEnabled(id uint, enabled bool) (*DownloadClientView, error) {
	client, err := s.repo.GetClient(id)
	if err != nil {
		return nil, err
	}
	client.Enabled = enabled
	if err := s.repo.UpdateClient(client); err != nil {
		return nil, err
	}
	view, err := s.view(*client)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *DownloadClientService) build(base *entity.SearchDownloadClient, input DownloadClientInput, creating bool) (*entity.SearchDownloadClient, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, &InputError{Field: "name", Message: "名称不能为空"}
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = "qbittorrent"
	}
	if kind != "qbittorrent" {
		return nil, &InputError{Field: "kind", Message: "当前只支持 qBittorrent"}
	}
	host := strings.TrimSpace(input.Host)
	if host == "" {
		return nil, &InputError{Field: "host", Message: "地址不能为空"}
	}
	port := input.Port
	if port == 0 {
		port = 8080
	}
	if port < 1 || port > 65535 {
		return nil, &InputError{Field: "port", Message: "端口范围 1–65535"}
	}

	// On update an empty field means "keep the stored secret": the form never
	// pre-fills it, so treating blank as "clear" silently broke every saved
	// password (BUG-016). Clearing requires the explicit sentinel.
	apiKeyPlain := internalcrypto.ResolveForUpdate(input.APIKey, mustDecrypt(s.box, base.APIKeyEnc))
	passwordPlain := internalcrypto.ResolveForUpdate(input.Password, mustDecrypt(s.box, base.PasswordEnc))

	username := input.Username
	if username == "" && !creating {
		username = base.Username
	}
	// API key and username/password are mutually exclusive.
	if apiKeyPlain != "" && (username != "" || passwordPlain != "") {
		return nil, &InputError{Field: "api_key", Message: "API Key 与账号密码只能二选一"}
	}

	initialState := strings.TrimSpace(input.InitialState)
	if initialState == "" {
		initialState = "start"
	}
	switch initialState {
	case "start", "pause", "force":
	default:
		return nil, &InputError{Field: "initial_state", Message: "初始状态只支持 start/pause/force"}
	}

	layout := strings.TrimSpace(input.ContentLayout)
	if layout == "" {
		layout = "original"
	}
	switch layout {
	case "original", "no-subfolder", "subfolder":
	default:
		return nil, &InputError{Field: "content_layout", Message: "内容布局取值非法"}
	}

	apiKeyEnc, err := s.box.Encrypt(apiKeyPlain)
	if err != nil {
		return nil, err
	}
	passwordEnc, err := s.box.Encrypt(passwordPlain)
	if err != nil {
		return nil, err
	}

	out := *base
	out.Name = name
	out.Kind = kind
	out.Host = host
	out.Port = port
	out.UseSSL = input.UseSSL
	out.URLBase = strings.Trim(strings.TrimSpace(input.URLBase), "/")
	out.APIKeyEnc = apiKeyEnc
	out.Username = username
	out.PasswordEnc = passwordEnc
	out.DefaultCategory = strings.TrimSpace(input.DefaultCategory)
	out.InitialState = initialState
	out.Sequential = input.Sequential
	out.FirstLastFirst = input.FirstLastFirst
	out.ContentLayout = layout
	out.Priority = input.Priority
	if out.Priority <= 0 {
		out.Priority = 50
	}
	if out.Priority > 100 {
		return nil, &InputError{Field: "priority", Message: "优先级范围 1–100"}
	}
	out.IsDefault = input.IsDefault
	if input.Enabled != nil {
		out.Enabled = *input.Enabled
	} else if creating {
		out.Enabled = true
	}
	return &out, nil
}

func (s *DownloadClientService) replaceMappings(clientID uint, mappings []CategoryMappingInput) error {
	seen := map[int]bool{}
	rows := make([]entity.SearchDownloadClientCategory, 0, len(mappings))
	for _, m := range mappings {
		if m.SourceCategory <= 0 {
			return &InputError{Field: "categories", Message: "来源分类必须是正整数"}
		}
		if strings.TrimSpace(m.ClientCategory) == "" {
			return &InputError{Field: "categories", Message: "下载器分类不能为空"}
		}
		if seen[m.SourceCategory] {
			return &InputError{Field: "categories", Message: fmt.Sprintf("来源分类 %d 重复配置", m.SourceCategory)}
		}
		seen[m.SourceCategory] = true
		rows = append(rows, entity.SearchDownloadClientCategory{
			SourceCategory: m.SourceCategory,
			ClientCategory: strings.TrimSpace(m.ClientCategory),
		})
	}
	return s.repo.ReplaceClientCategories(clientID, rows)
}

// Client builds a live client for one configured instance.
func (s *DownloadClientService) Client(client entity.SearchDownloadClient) qbittorrent.Client {
	return qbittorrent.NewHTTPClient(qbittorrent.Config{
		Host:     client.Host,
		Port:     client.Port,
		UseSSL:   client.UseSSL,
		URLBase:  client.URLBase,
		APIKey:   mustDecrypt(s.box, client.APIKeyEnc),
		Username: client.Username,
		Password: mustDecrypt(s.box, client.PasswordEnc),
		Proxy:    "",
	}, s.http)
}

// Categories lists the categories that exist in a configured download client, so
// the mapping UI can offer real names instead of free text.
func (s *DownloadClientService) Categories(ctx context.Context, id uint) ([]qbittorrent.CategoryInfo, error) {
	client, err := s.repo.GetClient(id)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	categories, err := s.Client(*client).Categories(callCtx)
	if err != nil {
		var qErr *qbittorrent.Error
		if errors.As(err, &qErr) {
			return nil, &InputError{Field: "client", Message: qErr.Message}
		}
		return nil, err
	}
	return categories, nil
}

// Test checks connectivity and creates missing categories (REQ-13).
func (s *DownloadClientService) Test(ctx context.Context, id uint) (*TestResult, error) {
	client, err := s.repo.GetClient(id)
	if err != nil {
		return nil, err
	}
	categories := s.requiredCategories(*client)
	start := time.Now()
	status, err := s.Client(*client).Test(ctx, categories)
	elapsed := time.Since(start)

	if err != nil {
		kind := "unreachable"
		var qErr *qbittorrent.Error
		if errors.As(err, &qErr) {
			kind = qErr.Kind
		}
		s.recordFailure(id)
		return &TestResult{OK: false, IndexerName: client.Name, ErrorKind: kind, Message: err.Error(), ElapsedMS: elapsed.Milliseconds()}, nil
	}

	s.recordSuccess(id)
	result := &TestResult{
		OK: true, IndexerName: client.Name, Protocol: "torrent",
		ElapsedMS: elapsed.Milliseconds(), Message: "版本 " + status.Version,
		CategoriesReady: status.CategoriesReady, CategoriesFailed: status.CategoriesFailed,
	}
	if len(status.CategoriesFailed) > 0 {
		result.OK = false
		result.ErrorKind = "category"
		result.Message = "分类不可用：" + strings.Join(status.CategoriesFailed, "、")
	}
	return result, nil
}

// requiredCategories returns the default category plus every mapped category.
func (s *DownloadClientService) requiredCategories(client entity.SearchDownloadClient) []string {
	set := map[string]bool{}
	out := []string{}
	if cat := strings.TrimSpace(client.DefaultCategory); cat != "" {
		set[cat] = true
		out = append(out, cat)
	}
	mappings, err := s.repo.ListClientCategories(client.ID)
	if err != nil {
		return out
	}
	for _, m := range mappings {
		if set[m.ClientCategory] {
			continue
		}
		set[m.ClientCategory] = true
		out = append(out, m.ClientCategory)
	}
	return out
}

// ResolveCategory implements BR-19 and reports only the resulting category.
func (s *DownloadClientService) ResolveCategory(clientID uint, releaseCategories []int) (string, error) {
	category, _, err := s.ResolveCategoryDetailed(clientID, releaseCategories)
	return category, err
}

// ListCategoryMappings returns the configured source→target mapping rows.
func (s *DownloadClientService) ListCategoryMappings(clientID uint) ([]entity.SearchDownloadClientCategory, error) {
	return s.repo.ListClientCategories(clientID)
}

// ResolveCategoryDetailed also reports which rule matched, so the confirmation
// dialog can explain the pre-filled category ("mapping" / "parent" / "default" /
// "none").
func (s *DownloadClientService) ResolveCategoryDetailed(clientID uint, releaseCategories []int) (string, string, error) {
	client, err := s.repo.GetClient(clientID)
	if err != nil {
		return "", "", err
	}
	mappings, err := s.repo.ListClientCategories(clientID)
	if err != nil {
		return "", "", err
	}

	// Exact match.
	for _, m := range mappings {
		for _, cat := range releaseCategories {
			if m.SourceCategory == cat {
				return m.ClientCategory, "mapping", nil
			}
		}
	}
	// Parent group: 2040 is covered by a 2000 mapping.
	for _, m := range mappings {
		for _, cat := range releaseCategories {
			if isParentCategory(m.SourceCategory, cat) {
				return m.ClientCategory, "parent", nil
			}
		}
	}
	if fallback := strings.TrimSpace(client.DefaultCategory); fallback != "" {
		return fallback, "default", nil
	}
	return "", "none", nil
}

func isParentCategory(parent, child int) bool {
	p := fmt.Sprint(parent)
	c := fmt.Sprint(child)
	if len(p) != 4 || len(c) != 4 || p == c {
		return false
	}
	return p[:2] == c[:2] && p[2:] == "00"
}

// Select picks the client for a grab (BR-20): bound client first, then enabled
// by priority, skipping temporarily unavailable instances.
func (s *DownloadClientService) Select(indexerDownloadClientID uint) (*entity.SearchDownloadClient, error) {
	if indexerDownloadClientID > 0 {
		client, err := s.repo.GetClient(indexerDownloadClientID)
		if err == nil && client.Enabled && !client.UnavailableUntil.After(time.Now()) {
			return client, nil
		}
	}

	clients, err := s.repo.EnabledClients()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	available := make([]entity.SearchDownloadClient, 0, len(clients))
	for _, c := range clients {
		if c.UnavailableUntil.After(now) {
			continue
		}
		available = append(available, c)
	}
	if len(available) == 0 {
		if len(clients) > 0 {
			return nil, &InputError{Field: "download_client", Message: "所有下载器暂时不可用"}
		}
		return nil, &InputError{Field: "download_client", Message: "尚未配置下载器"}
	}

	best := available[0]
	for _, c := range available[1:] {
		if c.Priority < best.Priority {
			best = c
		}
		if c.Priority == best.Priority && c.IsDefault && !best.IsDefault {
			best = c
		}
	}
	return &best, nil
}

func (s *DownloadClientService) recordSuccess(id uint) {
	client, err := s.repo.GetClient(id)
	if err != nil {
		return
	}
	client.ConsecutiveFailures = 0
	client.UnavailableUntil = time.Time{}
	client.LastSuccessAt = time.Now()
	_ = s.repo.UpdateClient(client)
}

func (s *DownloadClientService) recordFailure(id uint) {
	client, err := s.repo.GetClient(id)
	if err != nil {
		return
	}
	client.ConsecutiveFailures++
	if client.ConsecutiveFailures >= 3 {
		client.UnavailableUntil = time.Now().Add(5 * time.Minute)
	}
	_ = s.repo.UpdateClient(client)
}

// MarkUnavailable is used by GrabService when a push fails.
func (s *DownloadClientService) MarkUnavailable(id uint) { s.recordFailure(id) }

// MarkAvailable is used when a push succeeds.
func (s *DownloadClientService) MarkAvailable(id uint) { s.recordSuccess(id) }

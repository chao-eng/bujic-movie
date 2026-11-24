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
	"github.com/bujic-movie/bujic-movie/internal/service/downloadclient"
	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
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
	DefaultSavePath string                 `json:"default_save_path"`
	InitialState    string                 `json:"initial_state"`
	Sequential      bool                   `json:"sequential"`
	FirstLastFirst  bool                   `json:"first_last_first"`
	ContentLayout   string                 `json:"content_layout"`
	Priority        int                    `json:"priority"`
	IsDefault       bool                   `json:"is_default"`
	Enabled         *bool                  `json:"enabled"`
	Categories      []CategoryMappingInput `json:"categories"`
}

// CategoryMappingInput is one source→client category mapping. SavePath is used
// only by clients without category-to-folder behaviour.
type CategoryMappingInput struct {
	SourceCategory int    `json:"source_category"`
	ClientCategory string `json:"client_category"`
	SavePath       string `json:"save_path"`
}

// DownloadClientView masks credentials.
type DownloadClientView struct {
	entity.SearchDownloadClient
	APIKeyMask    string                      `json:"api_key_mask"`
	PasswordMask  string                      `json:"password_mask"`
	Categories    []CategoryMappingInput      `json:"category_mappings"`
	UnavailableIn int64                       `json:"unavailable_remaining_ms"`
	Capabilities  downloadclient.Capabilities `json:"capabilities"`
}

// DownloadClientService owns client CRUD, category mapping, selection and
// failover. It never decides a save path for qBittorrent (D-04); for clients
// without category folders the mapping supplies an explicit directory instead.
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

// Kinds lists the download client kinds this build supports.
func (s *DownloadClientService) Kinds() []downloadclient.KindInfo {
	return downloadclient.Kinds()
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
	info, _ := downloadclient.LookupKind(client.Kind)
	mappings := make([]CategoryMappingInput, 0, len(cats))
	for _, c := range cats {
		mapping := CategoryMappingInput{SourceCategory: c.SourceCategory, ClientCategory: c.ClientCategory}
		// A directory stored before the kind stopped supporting one must not be
		// shown as if it were applied. Same for the client-wide default below.
		if info.Capabilities.SavePath {
			mapping.SavePath = c.SavePath
		}
		mappings = append(mappings, mapping)
	}
	remaining := int64(0)
	if client.UnavailableUntil.After(time.Now()) {
		remaining = time.Until(client.UnavailableUntil).Milliseconds()
	}
	if !info.Capabilities.SavePath {
		client.DefaultSavePath = ""
	}
	return DownloadClientView{
		SearchDownloadClient: client,
		APIKeyMask:           internalcrypto.Mask(apiKey),
		PasswordMask:         internalcrypto.Mask(password),
		Categories:           mappings,
		UnavailableIn:        remaining,
		Capabilities:         info.Capabilities,
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
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind == "" {
		if creating {
			kind = downloadclient.KindQBittorrent
		} else {
			kind = base.Kind
		}
	}
	info, ok := downloadclient.LookupKind(kind)
	if !ok {
		return nil, &InputError{Field: "kind", Message: "不支持的下载器类型，可选：" + downloadclient.KindNames()}
	}
	host := strings.TrimSpace(input.Host)
	if host == "" {
		return nil, &InputError{Field: "host", Message: "地址不能为空"}
	}
	port := input.Port
	if port == 0 {
		port = info.DefaultPort
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
	// API key and username/password are mutually exclusive, and only qBittorrent
	// understands an API key at all.
	switch info.Auth {
	case downloadclient.AuthAPIKeyOrUserPass:
		if apiKeyPlain != "" && (username != "" || passwordPlain != "") {
			return nil, &InputError{Field: "api_key", Message: "API Key 与账号密码只能二选一"}
		}
	case downloadclient.AuthUserPass:
		// Both fields are optional: Transmission accepts requests from its RPC
		// whitelist without any credentials.
		apiKeyPlain = ""
	case downloadclient.AuthPassword:
		apiKeyPlain = ""
		username = ""
		if passwordPlain == "" {
			return nil, &InputError{Field: "password", Message: info.Name + " 需要 Web UI 密码"}
		}
	case downloadclient.AuthToken:
		apiKeyPlain = ""
		username = ""
		// Aria2 runs without a token when it is bound to localhost; only warn by
		// leaving it empty rather than refusing to save.
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
	if !info.Capabilities.ContentLayout {
		layout = "original"
	}
	switch layout {
	case "original", "no-subfolder", "subfolder":
	default:
		return nil, &InputError{Field: "content_layout", Message: "内容布局取值非法"}
	}

	if !info.Capabilities.Categories {
		// Without labels a mapping cannot carry a category name; only its save
		// path is meaningful, and a mapping without one could never be applied.
		for _, m := range input.Categories {
			if strings.TrimSpace(m.SavePath) == "" {
				return nil, &InputError{Field: "categories", Message: info.Name + " 不支持分类，请为每条映射填写下载目录"}
			}
		}
	}
	if info.Capabilities.Categories && !info.Capabilities.SavePath {
		// This kind derives the folder from the category and ignores a directory,
		// so accepting one would store a setting that never takes effect.
		for _, m := range input.Categories {
			if strings.TrimSpace(m.SavePath) != "" {
				return nil, &InputError{
					Field:   "categories",
					Message: info.Name + " 的保存目录由分类决定，不支持单独指定下载目录",
				}
			}
		}
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
	if !info.Capabilities.Categories {
		// Keep the column consistent with the capability: a client without
		// labels must not carry a stale category name from a previous kind.
		out.DefaultCategory = ""
	}
	if info.Capabilities.SavePath {
		out.DefaultSavePath = normalizeSavePath(input.DefaultSavePath)
	} else {
		// Keep the column honest: a kind that cannot pin a directory must not
		// keep a stale one from a previous kind, or the UI would show it as if
		// it were used.
		out.DefaultSavePath = ""
	}
	out.InitialState = initialState
	out.Sequential = input.Sequential && info.Capabilities.Queue
	out.FirstLastFirst = input.FirstLastFirst && info.Capabilities.Queue
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
	client, err := s.repo.GetClient(clientID)
	if err != nil {
		return err
	}
	info, _ := downloadclient.LookupKind(client.Kind)
	seen := map[int]bool{}
	rows := make([]entity.SearchDownloadClientCategory, 0, len(mappings))
	for _, m := range mappings {
		if m.SourceCategory <= 0 {
			return &InputError{Field: "categories", Message: "来源分类必须是正整数"}
		}
		category := strings.TrimSpace(m.ClientCategory)
		if !info.Capabilities.Categories {
			category = ""
		}
		savePath := normalizeSavePath(m.SavePath)
		if !info.Capabilities.SavePath {
			savePath = ""
		}
		if category == "" && savePath == "" {
			return &InputError{Field: "categories", Message: "下载器分类或下载目录至少填一个"}
		}
		if seen[m.SourceCategory] {
			return &InputError{Field: "categories", Message: fmt.Sprintf("来源分类 %d 重复配置", m.SourceCategory)}
		}
		seen[m.SourceCategory] = true
		rows = append(rows, entity.SearchDownloadClientCategory{
			SourceCategory: m.SourceCategory,
			ClientCategory: category,
			SavePath:       savePath,
		})
	}
	return s.repo.ReplaceClientCategories(clientID, rows)
}

// Client builds a live client for one configured instance.
func (s *DownloadClientService) Client(client entity.SearchDownloadClient) downloadclient.Client {
	return downloadclient.New(downloadclient.Config{
		Kind:     client.Kind,
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
func (s *DownloadClientService) Categories(ctx context.Context, id uint) ([]downloadclient.CategoryInfo, error) {
	client, err := s.repo.GetClient(id)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	categories, err := s.Client(*client).Categories(callCtx)
	if err != nil {
		var cErr *downloadclient.Error
		if downloadclient.As(err, &cErr) {
			return nil, &InputError{Field: "client", Message: cErr.Message}
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
		var cErr *downloadclient.Error
		if downloadclient.As(err, &cErr) {
			kind = cErr.Kind
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
		category := strings.TrimSpace(m.ClientCategory)
		if category == "" || set[category] {
			continue
		}
		set[category] = true
		out = append(out, category)
	}
	return out
}

// ResolveCategory implements BR-19 and reports only the resulting category.
func (s *DownloadClientService) ResolveCategory(clientID uint, releaseCategories []int) (string, error) {
	target, err := s.ResolveTarget(clientID, releaseCategories)
	if err != nil {
		return "", err
	}
	return target.Category, nil
}

// ResolveTarget is the category and save path a grab should use, plus which
// rule produced them.
//
// A download directory is only resolved for kinds whose SavePath capability is
// set. qBittorrent is not one of them: its torrents are under automatic
// management, where the category decides the folder and a savepath is ignored.
// Leaving the path empty there is not a fallback — it is the only correct value.
func (s *DownloadClientService) ResolveTarget(clientID uint, releaseCategories []int) (*GrabTarget, error) {
	client, err := s.repo.GetClient(clientID)
	if err != nil {
		return nil, err
	}
	info, _ := downloadclient.LookupKind(client.Kind)
	target := &GrabTarget{Source: "none"}

	if info.Capabilities.Categories {
		category, source, err := s.resolveMapped(client, releaseCategories)
		if err != nil {
			return nil, err
		}
		target.Category = category
		target.Source = source
	}
	if !info.Capabilities.SavePath {
		return target, nil
	}

	if path := s.mappedSavePath(client.ID, releaseCategories); path != "" {
		target.SavePath = path
		if target.Source == "none" {
			target.Source = "mapping"
		}
	} else if !(info.Capabilities.CategoryPath && target.Category != "") {
		// A category that already decides the folder must not be silently
		// overridden by the client-wide default; an explicit mapping path above
		// is the user's override.
		if defaultPath := normalizeSavePath(client.DefaultSavePath); defaultPath != "" {
			target.SavePath = defaultPath
			if target.Source == "none" {
				target.Source = "default"
			}
		}
	}
	return target, nil
}

// resolveMapped applies the exact → parent → default category rule.
func (s *DownloadClientService) resolveMapped(client *entity.SearchDownloadClient, releaseCategories []int) (string, string, error) {
	mappings, err := s.repo.ListClientCategories(client.ID)
	if err != nil {
		return "", "", err
	}
	for _, m := range mappings {
		if strings.TrimSpace(m.ClientCategory) == "" {
			continue
		}
		for _, cat := range releaseCategories {
			if m.SourceCategory == cat {
				return m.ClientCategory, "mapping", nil
			}
		}
	}
	// Parent group: 2040 is covered by a 2000 mapping.
	for _, m := range mappings {
		if strings.TrimSpace(m.ClientCategory) == "" {
			continue
		}
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

// mappedSavePath finds the directory configured for the release's category.
func (s *DownloadClientService) mappedSavePath(clientID uint, releaseCategories []int) string {
	mappings, err := s.repo.ListClientCategories(clientID)
	if err != nil {
		return ""
	}
	for _, m := range mappings {
		if normalizeSavePath(m.SavePath) == "" {
			continue
		}
		for _, cat := range releaseCategories {
			if m.SourceCategory == cat {
				return normalizeSavePath(m.SavePath)
			}
		}
	}
	for _, m := range mappings {
		if normalizeSavePath(m.SavePath) == "" {
			continue
		}
		for _, cat := range releaseCategories {
			if isParentCategory(m.SourceCategory, cat) {
				return normalizeSavePath(m.SavePath)
			}
		}
	}
	return ""
}

// ListCategoryMappings returns the configured source→target mapping rows.
func (s *DownloadClientService) ListCategoryMappings(clientID uint) ([]entity.SearchDownloadClientCategory, error) {
	return s.repo.ListClientCategories(clientID)
}

// ResolveCategoryDetailed also reports which rule matched, so the confirmation
// dialog can explain the pre-filled category ("mapping" / "parent" / "default" /
// "none").
func (s *DownloadClientService) ResolveCategoryDetailed(clientID uint, releaseCategories []int) (string, string, error) {
	target, err := s.ResolveTarget(clientID, releaseCategories)
	if err != nil {
		return "", "", err
	}
	return target.Category, target.Source, nil
}

// GrabTarget is the resolved downloader destination for one release.
type GrabTarget struct {
	Category string `json:"category"`
	SavePath string `json:"save_path"`
	Source   string `json:"source"` // mapping / parent / default / none
}

func isParentCategory(parent, child int) bool {
	p := fmt.Sprint(parent)
	c := fmt.Sprint(child)
	if len(p) != 4 || len(c) != 4 || p == c {
		return false
	}
	return p[:2] == c[:2] && p[2:] == "00"
}

func normalizeSavePath(path string) string {
	return strings.TrimRight(strings.TrimSpace(path), "/")
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

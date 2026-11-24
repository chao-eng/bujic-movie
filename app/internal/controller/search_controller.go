package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/bujic-movie/bujic-movie/internal/service"
	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/bujic-movie/bujic-movie/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SearchController exposes the search/download module over REST. Every handler
// requires JWT (enforced by the route group), and responses never contain
// plaintext credentials.
type SearchController struct {
	defs     *service.DefinitionService
	indexers *service.IndexerService
	clients  *service.DownloadClientService
	search   *service.SearchService
	grabs    *service.GrabService
	tasks    *service.TaskService
	titles   *service.TitleAliasService
	enabled  bool
}

// NewSearchController wires the module.
func NewSearchController(
	defs *service.DefinitionService,
	indexers *service.IndexerService,
	clients *service.DownloadClientService,
	search *service.SearchService,
	grabs *service.GrabService,
	tasks *service.TaskService,
	enabled bool,
) *SearchController {
	return &SearchController{defs: defs, indexers: indexers, clients: clients, search: search, grabs: grabs, tasks: tasks, enabled: enabled}
}

// SetTitleAliases attaches the resolver used by the title confirmation API.
func (h *SearchController) SetTitleAliases(titles *service.TitleAliasService) {
	h.titles = titles
}

// Status reports whether the module is enabled (API-22).
func (h *SearchController) Status(c *gin.Context) {
	response.Success(c, gin.H{"enabled": h.enabled})
}

// ListDefinitions returns the definition library (API-01).
func (h *SearchController) ListDefinitions(c *gin.Context) {
	list, err := h.defs.List()
	if err != nil {
		response.InternalServerError(c, "读取定义库失败")
		return
	}
	response.Success(c, gin.H{"items": list, "total": len(list)})
}

// GetDefinition returns one definition (API-04).
func (h *SearchController) GetDefinition(c *gin.Context) {
	view, err := h.defs.Get(c.Param("id"))
	if err != nil {
		response.NotFound(c, "定义不存在")
		return
	}
	response.Success(c, view)
}

// SyncDefinitions pulls the upstream library (API-02).
func (h *SearchController) SyncDefinitions(c *gin.Context) {
	summary, err := h.defs.Sync(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusBadGateway, 50201, err.Error())
		return
	}
	response.Success(c, summary)
}

// ImportDefinition stores one locally supplied YAML (API-03).
func (h *SearchController) ImportDefinition(c *gin.Context) {
	fileName := "imported.yml"
	var raw []byte

	if file, header, err := c.Request.FormFile("file"); err == nil {
		defer file.Close()
		content, readErr := io.ReadAll(io.LimitReader(file, 2<<20))
		if readErr != nil {
			response.BadRequest(c, "读取文件失败")
			return
		}
		raw = content
		if header != nil && strings.TrimSpace(header.Filename) != "" {
			fileName = header.Filename
		}
	} else {
		var body struct {
			FileName string `json:"file_name"`
			Content  string `json:"content"`
		}
		if bindErr := c.ShouldBindJSON(&body); bindErr != nil {
			response.BadRequest(c, "请上传 YAML 文件或提供 content 字段")
			return
		}
		if strings.TrimSpace(body.FileName) != "" {
			fileName = body.FileName
		}
		raw = []byte(body.Content)
	}

	summary, importErr := h.defs.Import(fileName, raw)
	if importErr != nil {
		h.writeServiceError(c, importErr)
		return
	}
	if summary.Invalid > 0 && summary.Added == 0 && summary.Updated == 0 {
		response.Error(c, http.StatusUnprocessableEntity, 40002, summary.Message)
		return
	}
	response.Success(c, summary)
}

// DeleteDefinition removes a definition (BR-38 keeps instances read-only).
func (h *SearchController) DeleteDefinition(c *gin.Context) {
	if err := h.defs.Delete(c.Param("id")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(c, "定义不存在")
			return
		}
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// ListIndexers returns every configured indexer (API-05).
func (h *SearchController) ListIndexers(c *gin.Context) {
	list, err := h.indexers.List()
	if err != nil {
		response.InternalServerError(c, "读取站点失败")
		return
	}
	response.Success(c, gin.H{"items": list, "total": len(list)})
}

// GetIndexer returns one indexer (API-05).
func (h *SearchController) GetIndexer(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	view, err := h.indexers.Get(id)
	if err != nil {
		response.NotFound(c, "站点不存在")
		return
	}
	response.Success(c, view)
}

// CreateIndexer adds an indexer (API-06).
func (h *SearchController) CreateIndexer(c *gin.Context) {
	var input service.IndexerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	view, err := h.indexers.Create(input)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// UpdateIndexer edits an indexer (API-07).
func (h *SearchController) UpdateIndexer(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input service.IndexerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	view, err := h.indexers.Update(id, input)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// SetIndexerEnabled toggles an indexer (API-07).
func (h *SearchController) SetIndexerEnabled(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	view, err := h.indexers.SetEnabled(id, body.Enabled)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// DeleteIndexer removes an indexer (API-08).
func (h *SearchController) DeleteIndexer(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.indexers.Delete(id); err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// TestIndexer runs a connectivity test (API-09).
func (h *SearchController) TestIndexer(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	result, err := h.indexers.Test(c.Request.Context(), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	if !result.OK {
		h.writeTestFailure(c, result)
		return
	}
	response.Success(c, result)
}

// TestAllIndexers runs a connectivity test for every site and returns one fresh
// row payload per site. The UI patches its list from this instead of reloading.
func (h *SearchController) TestAllIndexers(c *gin.Context) {
	items := h.indexers.TestAll(c.Request.Context())
	succeeded := 0
	failed := 0
	for _, item := range items {
		if item.OK {
			succeeded++
		} else if item.Message != "" {
			failed++
		}
	}
	response.Success(c, gin.H{
		"items":     items,
		"total":     len(items),
		"succeeded": succeeded,
		"failed":    failed,
	})
}

// SubmitCaptcha completes a captcha login (API-23).
func (h *SearchController) SubmitCaptcha(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var body struct {
		Captcha string `json:"captcha"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	result, err := h.indexers.SubmitCaptcha(c.Request.Context(), id, body.Captcha)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	if !result.OK {
		h.writeTestFailure(c, result)
		return
	}
	response.Success(c, result)
}

// RefreshDefinition switches an instance to the newest definition (API-10).
func (h *SearchController) RefreshDefinition(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	view, err := h.indexers.RefreshDefinition(id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// ClearCooldown resumes a cooled-down site early (API-22).
func (h *SearchController) ClearCooldown(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.indexers.ClearCooldown(id); err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"cleared": true})
}

// ListClients returns download clients (API-11).
func (h *SearchController) ListClients(c *gin.Context) {
	list, err := h.clients.List()
	if err != nil {
		response.InternalServerError(c, "读取下载器失败")
		return
	}
	response.Success(c, gin.H{"items": list, "total": len(list)})
}

// ClientKinds lists the supported download client kinds and their capabilities
// (API-30), so the form can show only the fields a kind actually uses.
func (h *SearchController) ClientKinds(c *gin.Context) {
	response.Success(c, gin.H{"items": h.clients.Kinds()})
}

// GetClient returns one download client (API-11).
func (h *SearchController) GetClient(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	view, err := h.clients.Get(id)
	if err != nil {
		response.NotFound(c, "下载器不存在")
		return
	}
	response.Success(c, view)
}

// CreateClient adds a download client (API-12).
func (h *SearchController) CreateClient(c *gin.Context) {
	var input service.DownloadClientInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	view, err := h.clients.Create(input)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// UpdateClient edits a download client (API-13).
func (h *SearchController) UpdateClient(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input service.DownloadClientInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	view, err := h.clients.Update(id, input)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// SetClientEnabled toggles a download client (API-13).
func (h *SearchController) SetClientEnabled(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	view, err := h.clients.SetEnabled(id, body.Enabled)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// DeleteClient removes a download client (API-14).
func (h *SearchController) DeleteClient(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.clients.Delete(id); err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// ClientCategories lists categories that exist in a download client (API-28).
func (h *SearchController) ClientCategories(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	categories, err := h.clients.Categories(c.Request.Context(), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"items": categories})
}

// TestClient runs a downloader connectivity test (API-15).
func (h *SearchController) TestClient(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	result, err := h.clients.Test(c.Request.Context(), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	if !result.OK {
		h.writeTestFailure(c, result)
		return
	}
	response.Success(c, result)
}

// Query runs an aggregated search (API-16).
func (h *SearchController) Query(c *gin.Context) {
	var body struct {
		RequestID  string `json:"request_id"`
		Keyword    string `json:"keyword"`
		Categories []int  `json:"categories"`
		IndexerIDs []uint `json:"indexer_ids"`
		Mode       string `json:"mode"`
		MinSeeders int    `json:"min_seeders"`
		MinSize    int64  `json:"min_size"`
		MaxSize    int64  `json:"max_size"`
		Page       int    `json:"page"`
		PageSize   int    `json:"page_size"`
		Sort       string `json:"sort"`
		// Title resolution controls: the UI confirms a TMDB match and re-runs
		// the query with the chosen aliases.
		MediaType     string   `json:"media_type"`
		Aliases       []string `json:"aliases"`
		TitleConfirmed bool    `json:"title_confirmed"`
		RefreshTitle  bool     `json:"refresh_title"`
		// TitleRecognition=false means "直接搜索": keep the typed keyword as-is.
		TitleRecognition *bool `json:"title_recognition"`
		// SiteLanguages limits the fan-out to sites in these languages (multi-select).
		SiteLanguages []string `json:"site_languages"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}

	result, err := h.search.Search(c.Request.Context(), service.SearchQuery{
		RequestID: body.RequestID, Keyword: service.NormaliseKeyword(body.Keyword),
		Categories: body.Categories, IndexerIDs: body.IndexerIDs, Mode: body.Mode,
		MinSeeders: body.MinSeeders, MinSize: body.MinSize, MaxSize: body.MaxSize,
		Page: body.Page, PageSize: body.PageSize, Sort: body.Sort,
		MediaType: body.MediaType, Aliases: body.Aliases, Confirmed: body.TitleConfirmed,
		RefreshTitles: body.RefreshTitle,
		// The UI sends false when its toggle is off; an absent field keeps the
		// existing behaviour so other callers are unaffected.
		SkipTitle: body.TitleRecognition != nil && !*body.TitleRecognition,
		SiteLanguages: body.SiteLanguages,
	})
	if err != nil {
		if errors.Is(err, service.ErrNoIndexers) {
			response.Error(c, http.StatusConflict, 40901, "没有可用的站点，请先添加并测试站点")
			return
		}
		response.Error(c, http.StatusTooManyRequests, 42901, err.Error())
		return
	}
	response.Success(c, result)
}

// TitleCandidates resolves a keyword to TMDB candidates for manual
// confirmation (API-31). It never queries indexers, so the UI can show the list
// before spending the search budget.
func (h *SearchController) TitleCandidates(c *gin.Context) {
	if h.titles == nil {
		response.Error(c, http.StatusServiceUnavailable, 50301, "片名解析不可用")
		return
	}
	var body struct {
		Keyword       string `json:"keyword"`
		MediaType     string `json:"media_type"`
		RefreshTitles bool   `json:"refresh"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	if strings.TrimSpace(body.Keyword) == "" {
		response.BadRequest(c, "关键词不能为空")
		return
	}
	resolution, err := h.titles.Resolve(c.Request.Context(), body.Keyword, body.MediaType, body.RefreshTitles)
	if err != nil {
		// Resolve degrades instead of failing, so this is defensive: a future
		// change that starts returning an error must not leak its text (it can
		// carry the TMDB URL) to the client.
		logger.Warn("search: title candidates failed: %v", err)
		response.Error(c, http.StatusBadGateway, 50203, "片名解析失败，请稍后重试")
		return
	}
	response.Success(c, resolution)
}

// ConfirmTitle stores the user's pick so later searches reuse it (API-32).
func (h *SearchController) ConfirmTitle(c *gin.Context) {
	if h.titles == nil {
		response.Error(c, http.StatusServiceUnavailable, 50301, "片名解析不可用")
		return
	}
	var body struct {
		Keyword   string `json:"keyword"`
		MediaType string `json:"media_type"`
		TMDBID    int    `json:"tmdb_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	if body.TMDBID <= 0 {
		response.BadRequest(c, "请选择一个 TMDB 结果")
		return
	}
	resolution, err := h.titles.Confirm(c.Request.Context(), body.Keyword, body.MediaType, body.TMDBID)
	if err != nil {
		logger.Warn("search: confirming title failed: %v", err)
		response.Error(c, http.StatusBadRequest, 40011, err.Error())
		return
	}
	response.Success(c, resolution)
}

// ClearTitle drops the cached resolution for a keyword (API-33).
func (h *SearchController) ClearTitle(c *gin.Context) {
	if h.titles == nil {
		response.Error(c, http.StatusServiceUnavailable, 50301, "片名解析不可用")
		return
	}
	keyword := c.Query("keyword")
	if strings.TrimSpace(keyword) == "" {
		response.BadRequest(c, "缺少 keyword 参数")
		return
	}
	if err := h.titles.Clear(keyword); err != nil {
		response.InternalServerError(c, "清除失败")
		return
	}
	response.Success(c, gin.H{"cleared": true})
}

// GrabPreview resolves the downloader and category for a result without pushing
// it, so the confirmation dialog can pre-fill the mapped category (API-29).
func (h *SearchController) GrabPreview(c *gin.Context) {
	var req service.GrabRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	preview, err := h.grabs.Preview(c.Request.Context(), req)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, preview)
}

// Grab pushes one result (API-17).
func (h *SearchController) Grab(c *gin.Context) {
	var req service.GrabRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	result, err := h.grabs.Grab(c.Request.Context(), req)
	if err != nil {
		var grabErr *service.GrabError
		if errors.As(err, &grabErr) {
			// Keep the record in the body so the UI can link to the task even on
			// failure (AC-40), but classify the status by failure kind.
			status, code := http.StatusBadGateway, 50202
			switch grabErr.Kind {
			case service.GrabKindDuplicate:
				status, code = http.StatusConflict, 40902
			case service.GrabKindPending:
				status, code = http.StatusGatewayTimeout, 50401
			}
			c.JSON(status, gin.H{"code": code, "msg": grabErr.Message, "data": result})
			return
		}
		h.writeServiceError(c, err)
		return
	}
	// A deduped grab is a conflict per the error table, while still returning
	// the existing task so the caller can navigate to it.
	if result != nil && result.Duplicate {
		c.JSON(http.StatusConflict, gin.H{"code": 40902, "msg": result.Message, "data": result})
		return
	}
	response.Success(c, result)
}

// Tasks aggregates tasks across clients (API-18).
func (h *SearchController) Tasks(c *gin.Context) {
	groups, err := h.tasks.Groups(c.Request.Context())
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"groups": groups})
}

// TaskAction pauses, resumes or deletes one task (API-19).
func (h *SearchController) TaskAction(c *gin.Context) {
	clientID, err := strconv.ParseUint(c.Param("client_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "下载器标识非法")
		return
	}
	hash := c.Param("hash")
	var body struct {
		Action      string `json:"action"`
		DeleteFiles bool   `json:"delete_files"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	if err := h.tasks.Action(c.Request.Context(), uint(clientID), hash, body.Action, body.DeleteFiles); err != nil {
		var unavailable *service.ClientUnavailable
		if errors.As(err, &unavailable) {
			response.Error(c, http.StatusBadGateway, 50202, "下载器不可达")
			return
		}
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"ok": true})
}

// Records lists download records (API-20).
func (h *SearchController) Records(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	list, err := h.grabs.Records(limit)
	if err != nil {
		response.InternalServerError(c, "读取下载记录失败")
		return
	}
	response.Success(c, gin.H{"items": list, "total": len(list)})
}

// History lists search keywords (API-21).
func (h *SearchController) History(c *gin.Context) {
	list, err := h.search.History()
	if err != nil {
		response.InternalServerError(c, "读取搜索历史失败")
		return
	}
	response.Success(c, gin.H{"items": list})
}

// DeleteHistory removes one keyword (API-21).
func (h *SearchController) DeleteHistory(c *gin.Context) {
	keyword := c.Query("keyword")
	if strings.TrimSpace(keyword) == "" {
		if err := h.search.ClearHistory(); err != nil {
			h.writeServiceError(c, err)
			return
		}
		response.Success(c, gin.H{"cleared": true})
		return
	}
	if err := h.search.DeleteHistory(keyword); err != nil {
		h.writeServiceError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// RecordEvent accepts one front-end event for NFR-11 (API-24).
func (h *SearchController) RecordEvent(c *gin.Context) {
	var body struct {
		Name       string `json:"name"`
		DurationMS int64  `json:"duration_ms"`
		Success    bool   `json:"success"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	switch body.Name {
	case "search_submit", "search_first_render", "grab_confirm", "grab_result":
	default:
		response.BadRequest(c, "未知事件")
		return
	}
	h.search.Metrics().RecordEvent(body.Name, body.DurationMS, body.Success)
	response.Success(c, gin.H{"recorded": true})
}

// Metrics returns the in-process counters and latency percentiles (API-25).
func (h *SearchController) Metrics(c *gin.Context) {
	response.Success(c, h.search.Metrics().Snapshot())
}

// TestProxy checks that the definition source is reachable through the current
// proxy. It only fetches the list, so it returns quickly (unlike a full sync).
func (h *SearchController) TestProxy(c *gin.Context) {
	result := h.defs.TestSource(c.Request.Context())
	response.Success(c, result)
}

// ImportCookies accepts a manual browser session from the companion extension
// or the settings form (API-26). It is the supported answer to a Cloudflare
// interactive challenge: the user clears it in their own browser, then the
// cookies and UA are stored for that indexer.
func (h *SearchController) ImportCookies(c *gin.Context) {
	var input service.CookieImportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "请求参数无法解析")
		return
	}
	result, err := h.indexers.ImportCookies(c.Request.Context(), input)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	if len(result.Candidates) > 0 {
		c.JSON(http.StatusConflict, gin.H{"code": 40903, "msg": result.Message, "data": result})
		return
	}
	if !result.Updated {
		response.Error(c, http.StatusNotFound, 40401, result.Message)
		return
	}
	response.Success(c, result)
}

// Health returns indexer health for the status panel (API-22).
func (h *SearchController) Health(c *gin.Context) {
	statuses, err := h.indexers.Statuses()
	if err != nil {
		response.InternalServerError(c, "读取站点状态失败")
		return
	}
	indexers, _ := h.indexers.List()
	languages, _ := h.indexers.LanguageFacets()
	response.Success(c, gin.H{
		"enabled": h.enabled, "indexers": indexers, "statuses": statuses,
		// Languages powers the multi-select filter on the search page.
		"languages": languages,
	})
}

func (h *SearchController) writeTestFailure(c *gin.Context, result *service.TestResult) {
	switch result.ErrorKind {
	case "captcha":
		response.Error(c, http.StatusUnprocessableEntity, 42201, result.Message)
	case "auth":
		// 401 would mean "your session is invalid" and make the SPA log the user
		// out. The user is authenticated here; the *target* rejected the
		// credentials, so this is a failed precondition on the target (422).
		// Code 40102 stays so the UI can still say "check the account".
		response.Error(c, http.StatusUnprocessableEntity, 40102, result.Message)
	case "parse":
		message := result.Message
		if result.Field != "" {
			message += "（字段：" + result.Field + "）"
		}
		response.Error(c, http.StatusUnprocessableEntity, 42202, message)
	case "challenge":
		response.Error(c, http.StatusUnprocessableEntity, 42204, result.Message)
	case "category":
		response.Error(c, http.StatusUnprocessableEntity, 42203, result.Message)
	case "timeout":
		response.Error(c, http.StatusGatewayTimeout, 50401, result.Message)
	default:
		response.Error(c, http.StatusBadGateway, 50201, result.Message)
	}
}

func (h *SearchController) writeServiceError(c *gin.Context, err error) {
	var inputErr *service.InputError
	if errors.As(err, &inputErr) {
		response.BadRequest(c, inputErr.Message)
		return
	}
	var incompatible *service.IncompatibleError
	if errors.As(err, &incompatible) {
		response.Error(c, http.StatusForbidden, 40301, incompatible.Message)
		return
	}
	var storageErr *service.StorageError
	if errors.As(err, &storageErr) {
		response.Error(c, http.StatusInternalServerError, 50001, "保存失败，请检查磁盘空间")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "资源不存在")
		return
	}
	response.InternalServerError(c, "操作失败")
}

func parseID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.BadRequest(c, "标识非法")
		return 0, false
	}
	return uint(id), true
}

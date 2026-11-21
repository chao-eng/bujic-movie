package connector

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/internal/service/cardigann"
	"github.com/bujic-movie/bujic-movie/internal/service/httpclient"
)

// SessionStore persists login cookies for a Cardigann indexer.
type SessionStore interface {
	Load(ctx context.Context) (cookies map[string]string, state string, err error)
	Save(ctx context.Context, cookies map[string]string, expiresAt time.Time) error
	MarkFailed(ctx context.Context, reason string) error
	MarkPending(ctx context.Context) error
}

// CardigannConnector interprets a YAML definition in-process.
type CardigannConnector struct {
	def      *cardigann.Definition
	name     string
	baseURL  string
	settings map[string]string // non-secret instance settings
	secrets  map[string]string // decrypted secret settings
	exec     *httpclient.Executor
	allowed  []string
	proxy    string
	limitMS  int
	store    SessionStore

	mu      sync.Mutex
	cookies map[string]string

	session Session
}

// NewCardigann builds a connector from a parsed definition plus instance config.
func NewCardigann(def *cardigann.Definition, name string, settings, secrets map[string]string, session SessionStore, exec *httpclient.Executor, proxyURL string, limitMS int) (*CardigannConnector, error) {
	if def == nil {
		return nil, NewError(KindUnsupported, "定义缺失", "definition", nil)
	}
	if len(def.Links) == 0 {
		return nil, NewError(KindUnsupported, "定义没有站点地址", "links", nil)
	}
	if limitMS <= 0 {
		limitMS = 2000
	}
	limitMS = def.MinIntervalMS(limitMS)

	// Instance settings can override the primary link.
	baseURL := strings.TrimRight(def.Links[0], "/")
	if override := strings.TrimSpace(settings["base_url"]); override != "" {
		baseURL = strings.TrimRight(override, "/")
	}
	// Instance settings override definition defaults. Several upstream
	// definitions rely on defaults such as `apiurl` and `filter-id`.
	effective := map[string]string{}
	for _, field := range def.Settings {
		if field.Default == "" {
			continue
		}
		effective[field.Name] = field.Default.String()
	}
	for key, value := range settings {
		effective[key] = value
	}

	allowed := httpclient.HostsFromLinks(def.Links)
	if base, err := url.Parse(baseURL); err == nil && base.Hostname() != "" {
		allowed = appendUnique(allowed, strings.ToLower(base.Hostname()))
	}
	// Some definitions call a separate API host declared as a setting
	// (Prowlarr's `apiurl`, for example apibay.org for The Pirate Bay).
	for key, value := range effective {
		switch strings.ToLower(key) {
		case "apiurl", "api_url", "apihost", "api_host", "baseurl", "base_url":
			allowed = appendUnique(allowed, hostOfSetting(value))
		}
	}

	return &CardigannConnector{
		def: def, name: name, baseURL: baseURL,
		settings: effective, secrets: secrets,
		exec: exec, allowed: allowed, proxy: proxyURL, limitMS: limitMS,
		store:   session,
		cookies: map[string]string{},
	}, nil
}

func hostOfSetting(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func appendUnique(list []string, value string) []string {
	for _, item := range list {
		if item == value {
			return list
		}
	}
	return append(list, value)
}

// SetSession installs the manually supplied browser session.
func (c *CardigannConnector) SetSession(session Session) { c.session = session }

func (c *CardigannConnector) Name() string {
	if c.name != "" {
		return c.name
	}
	return c.def.Name
}

func (c *CardigannConnector) rateKey() string { return "cardigann:" + c.baseURL }

func (c *CardigannConnector) config() map[string]string {
	out := make(map[string]string, len(c.settings)+len(c.secrets))
	for k, v := range c.settings {
		out[k] = v
	}
	for k, v := range c.secrets {
		out[k] = v
	}
	return out
}

// Test performs login (when required) and one probe search.
func (c *CardigannConnector) Test(ctx context.Context) (*Capabilities, error) {
	start := time.Now()
	loginState := "无需登录"

	if c.def.UsesLogin() {
		if c.def.NeedsCaptcha() {
			return nil, NewError(KindCaptcha, "该站点需要验证码，请先提交验证码", "login.captcha", ErrCaptchaRequired)
		}
		if err := c.login(ctx, "", false); err != nil {
			return nil, err
		}
		loginState = "已登录"
	}

	if _, err := c.Search(ctx, Query{Keyword: "", Limit: 1, Mode: "search"}); err != nil {
		// An empty result set is fine; a classified error means the probe failed.
		return nil, err
	}

	return &Capabilities{
		Name:         c.Name(),
		Protocol:     "torrent",
		Categories:   len(c.def.Caps.Categories) + len(c.def.Caps.CategoryMappings),
		SearchModes:  c.def.SearchModes(),
		NeedsLogin:   c.def.UsesLogin(),
		NeedsCaptcha: c.def.NeedsCaptcha(),
		Elapsed:      time.Since(start),
		LoginState:   loginState,
	}, nil
}

// LoginWithCaptcha submits a captcha and completes login (API-23).
func (c *CardigannConnector) LoginWithCaptcha(ctx context.Context, captcha string) error {
	if !c.def.UsesLogin() {
		return NewError(KindUnsupported, "该站点不需要登录", "login", nil)
	}
	return c.login(ctx, captcha, true)
}

// login authenticates against the site. force ignores any cached session, which
// is what the expired-session retry needs (AC-14).
func (c *CardigannConnector) login(ctx context.Context, captcha string, force bool) error {
	login := c.def.Login
	if login == nil {
		return nil
	}
	// `method: cookie` carries a pre-shared cookie instead of a login request.
	if strings.EqualFold(strings.TrimSpace(login.Method), "cookie") {
		if c.store != nil && !force {
			if cookies, state, err := c.store.Load(ctx); err == nil && state == "logged-in" && len(cookies) > 0 {
				c.setCookies(cookies)
				return nil
			}
		}
		cookies := map[string]string{}
		ctxData := cardigann.NewTemplateContext("", nil, nil, c.config())
		for _, key := range login.Inputs.Keys {
			tpl := login.Inputs.Values[key]
			rendered, err := cardigann.Render(tpl, ctxData)
			if err != nil || strings.TrimSpace(rendered) == "" {
				continue
			}
			cookies[key] = rendered
		}
		if len(cookies) == 0 {
			return NewError(KindAuth, "未配置站点 Cookie", "login.inputs.cookie", nil)
		}
		c.setCookies(cookies)
		if c.store != nil {
			_ = c.store.Save(ctx, cookies, time.Now().Add(30*24*time.Hour))
		}
		return nil
	}
	if login.Path == "" {
		return nil
	}
	if c.store != nil && !force {
		cookies, state, err := c.store.Load(ctx)
		if err == nil && state == "logged-in" && len(cookies) > 0 {
			c.setCookies(cookies)
			return nil
		}
	}

	form := url.Values{}
	ctxData := cardigann.NewTemplateContext("", nil, nil, c.config())
	ctxData.Result["captcha"] = captcha
	ctxData.Config["captcha"] = captcha

	for _, key := range login.Inputs.Keys {
		rendered, err := cardigann.Render(login.Inputs.Values[key], ctxData)
		if err != nil {
			return NewError(KindUnsupported, "登录模板渲染失败", "login.inputs."+key, err)
		}
		if rendered != "" {
			form.Set(key, rendered)
		}
	}
	if captcha != "" {
		form.Set("captcha", captcha)
	}

	method := strings.ToUpper(login.Method)
	if method == "" || method == "FORM" || method == "POST" {
		method = "POST"
	}

	target := c.baseURL + login.Path
	req := httpclient.Request{
		Method:    method,
		URL:       target,
		Header:    c.withSession(map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Referer": c.baseURL + "/"}),
		Allowed:   c.allowed,
		RateKey:   c.rateKey(),
		RateLimit: time.Duration(c.limitMS) * time.Millisecond,
		Proxy:     c.proxy,
		NoRetry:   true,
	}
	if method == "GET" {
		if encoded := form.Encode(); encoded != "" {
			req.URL = target + "?" + encoded
		}
	} else {
		req.Body = strings.NewReader(form.Encode())
	}

	resp, err := c.exec.Do(ctx, req)
	if err != nil {
		if c.store != nil {
			_ = c.store.MarkFailed(ctx, "登录请求失败")
		}
		return translateHTTPError(err)
	}

	cookies := cookiesFromResponse(resp)
	if len(cookies) == 0 {
		// Some sites return cookies on the login page itself (get method).
		cookies = c.snapshotCookies()
	}
	c.setCookies(cookies)

	if c.loginFailed(resp.Body) {
		if c.store != nil {
			_ = c.store.MarkFailed(ctx, "账号或密码错误")
		}
		return NewError(KindAuth, "账号或密码错误", "login", nil)
	}

	if c.store != nil {
		_ = c.store.Save(ctx, cookies, time.Now().Add(12*time.Hour))
	}
	return nil
}

func (c *CardigannConnector) loginFailed(body []byte) bool {
	if c.def.Login == nil {
		return false
	}
	for _, errBlock := range c.def.Login.Error {
		if errBlock.Selector == "" {
			continue
		}
		if containsSelector(body, errBlock.Selector) {
			return true
		}
	}
	return false
}

// Search renders every applicable path, runs them, and merges releases.
func (c *CardigannConnector) Search(ctx context.Context, q Query) ([]Release, error) {
	paths := c.applicablePaths(q)
	if len(paths) == 0 {
		return nil, NewError(KindUnsupported, "该站点不支持所选分类", "caps.categories", nil)
	}

	// Each path gets an equal share of the 8s budget, with a 2s floor.
	budget := pathBudget(len(paths))
	var all []Release
	var firstErr error
	for _, path := range paths {
		releases, err := c.runPath(ctx, path, q, budget)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		all = append(all, releases...)
	}
	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}

	for i := range all {
		all[i].IndexerName = c.Name()
	}
	return all, nil
}

func pathBudget(paths int) time.Duration {
	if paths <= 1 {
		return httpclient.DefaultTimeout
	}
	share := httpclient.DefaultTimeout / time.Duration(paths)
	if share < 2*time.Second {
		return 2 * time.Second
	}
	return share
}

func (c *CardigannConnector) applicablePaths(q Query) []cardigann.SearchPathBlock {
	if c.def.Search == nil {
		return nil
	}
	var out []cardigann.SearchPathBlock
	for _, path := range c.def.Search.Paths {
		if len(path.Categories) > 0 && len(q.Categories) > 0 && !intersectsAny(path.Categories, q.Categories) {
			continue
		}
		out = append(out, path)
	}
	if len(out) == 0 && len(c.def.Search.Paths) == 0 && c.def.Search.Path != "" {
		out = append(out, cardigann.SearchPathBlock{RequestBlock: cardigann.RequestBlock{Path: c.def.Search.Path}})
	}
	return out
}

func intersectsAny(pathCats []string, queryCats []int) bool {
	set := map[string]bool{}
	for _, v := range pathCats {
		set[strings.TrimSpace(v)] = true
	}
	for _, cat := range queryCats {
		if set[fmt.Sprint(cat)] {
			return true
		}
		for key := range set {
			// A path may declare a group like 2000 while the query uses 2040.
			if len(key) == 4 && len(fmt.Sprint(cat)) == 4 && strings.HasPrefix(fmt.Sprint(cat), key[:2]) {
				return true
			}
		}
	}
	return false
}

func (c *CardigannConnector) runPath(ctx context.Context, path cardigann.SearchPathBlock, q Query, budget time.Duration) ([]Release, error) {
	// Preserve declaration order: the site's WAF can depend on it.
	inputs := cardigann.OrderedInputs{Keys: nil, Values: map[string]string{}}
	for _, k := range c.def.Search.Inputs.Keys {
		inputs.Keys = append(inputs.Keys, k)
		inputs.Values[k] = c.def.Search.Inputs.Values[k]
	}
	if path.InheritInputs == nil || *path.InheritInputs {
		for _, k := range path.Inputs.Keys {
			if _, exists := inputs.Values[k]; !exists {
				inputs.Keys = append(inputs.Keys, k)
			}
			inputs.Values[k] = path.Inputs.Values[k]
		}
	}

	categories := make([]string, 0, len(q.Categories))
	for _, v := range q.Categories {
		categories = append(categories, fmt.Sprint(v))
	}
	if len(path.Categories) > 0 {
		categories = path.Categories
	}

	// Keyword filters run on the user's term before it reaches the template.
	// They matter: some sites cannot handle CJK, and definitions rewrite the
	// term to something the site accepts.
	keyword := q.Keyword
	if len(c.def.Search.KeywordsFilters) > 0 {
		if filtered, err := cardigann.ApplyFilters(keyword, c.def.Search.KeywordsFilters); err == nil {
			keyword = filtered
		}
	}

	ctxData := cardigann.NewTemplateContext(keyword, categories, map[string]string{
		"page": fmt.Sprint(q.Page),
	}, c.config())
	ctxData.Result = map[string]string{
		"season":  fmt.Sprint(q.Season),
		"episode": fmt.Sprint(q.Episode),
		"year":    fmt.Sprint(q.Year),
	}

	reqBlock := cardigann.RequestBlock{Path: path.Path, Method: path.Method, Inputs: inputs, QuerySeparator: path.QuerySeparator}
	target, err := cardigann.BuildQueryURL(c.baseURL, reqBlock, ctxData)
	if err != nil {
		return nil, NewError(KindUnsupported, "查询模板渲染失败", "search.inputs", err)
	}

	headers := map[string]string{"Referer": c.baseURL + "/"}
	for _, key := range sortedKeys(c.def.Search.Headers) {
		if vals := c.def.Search.Headers[key]; len(vals) > 0 {
			headers[key] = vals[0]
		}
	}
	if cookie := c.cookieHeader(); cookie != "" {
		headers["Cookie"] = cookie
	}
	headers = c.withSession(headers)

	callCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	resp, err := c.exec.Do(callCtx, httpclient.Request{
		URL:       target,
		Header:    headers,
		Timeout:   budget,
		Allowed:   c.allowed,
		RateKey:   c.rateKey(),
		RateLimit: time.Duration(c.limitMS) * time.Millisecond,
		Proxy:     c.proxy,
	})
	if err != nil {
		if needsRelogin(err) && c.def.UsesLogin() {
			// Session may have expired: force a fresh login and retry once (AC-14).
			if reloginErr := c.login(ctx, "", true); reloginErr == nil {
				headers["Cookie"] = c.cookieHeader()
				resp, err = c.exec.Do(callCtx, httpclient.Request{
					URL: target, Header: headers, Timeout: budget, Allowed: c.allowed,
					RateKey: c.rateKey(), RateLimit: time.Duration(c.limitMS) * time.Millisecond, Proxy: c.proxy,
				})
			}
		}
		if err != nil {
			return nil, translateHTTPError(err)
		}
	}

	responseType := ""
	if path.Response != nil {
		responseType = path.Response.Type
		if path.Response.NoResultsMessage != "" && strings.Contains(string(resp.Body), path.Response.NoResultsMessage) {
			return nil, nil
		}
	}

	body := resp.Body
	if len(c.def.Search.PreprocessingFilters) > 0 {
		if preprocessed, filterErr := cardigann.ApplyFilters(string(body), c.def.Search.PreprocessingFilters); filterErr == nil {
			body = []byte(preprocessed)
		}
	}

	parsed, err := cardigann.ExtractWithConfig(c.def, body, responseType, c.config())
	if err != nil {
		return nil, NewError(KindParse, err.Error(), extractField(err), err)
	}
	releases := make([]Release, 0, len(parsed))
	for _, rel := range parsed {
		releases = append(releases, Release{
			Title:        rel.Title,
			DownloadURL:  absoluteURL(c.baseURL, rel.DownloadURL),
			InfoHash:     rel.InfoHash,
			SizeBytes:    rel.SizeBytes,
			Seeders:      rel.Seeders,
			Peers:        rel.Peers,
			PublishDate:  rel.PublishDate,
			Categories:   rel.Categories,
			DownloadType: rel.DownloadType,
			IndexerName:  c.Name(),
			DetailsURL:   absoluteURL(c.baseURL, rel.DetailsURL),
		})
	}
	return releases, nil
}

func extractField(err error) string {
	if e, ok := err.(*cardigann.ExtractionError); ok {
		return e.Field
	}
	return "search"
}

// SupportsCategories reports whether the definition can serve any of the
// requested Newznab categories (REQ-16 / AC-27). Unknown names are treated as
// supported so a mapping gap never hides a site.
func (c *CardigannConnector) SupportsCategories(categories []int) bool {
	if len(categories) == 0 {
		return true
	}
	supported := map[int]bool{}
	for _, mapping := range c.def.Caps.CategoryMappings {
		if id, ok := newznabID(mapping.Cat); ok {
			supported[id] = true
		}
	}
	for name := range c.def.Caps.Categories {
		if id, ok := newznabID(name); ok {
			supported[id] = true
		}
	}
	if len(supported) == 0 {
		return true
	}
	for _, want := range categories {
		for have := range supported {
			if have == want || (have%100 == 0 && have/100 == want/100) {
				return true
			}
		}
	}
	return false
}

// newznabID maps a Cardigann category name (e.g. "Movies/HD") to its Newznab id.
func newznabID(name string) (int, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	table := map[string]int{
		"movies": 2000, "movies/hd": 2040, "movies/uhd": 2045, "movies/bluray": 2050,
		"movies/web-dl": 2045, "movies/dvd": 2010, "movies/sd": 2030, "movies/foreign": 2010,
		"tv": 5000, "tv/hd": 5040, "tv/uhd": 5045, "tv/sd": 5030, "tv/anime": 5070,
		"anime": 5070, "audio": 3000, "audio/mp3": 3010, "audio/lossless": 3040,
		"console": 1000, "pc": 4000, "books": 7000, "books/ebook": 7020,
	}
	id, ok := table[key]
	return id, ok
}

// ResolveDownload follows the definition download block: it may need a detail
// page before the torrent link can be read.
func (c *CardigannConnector) ResolveDownload(ctx context.Context, rel Release) (*DownloadRef, error) {
	if rel.DownloadURL != "" && rel.InfoHash != "" {
		return &DownloadRef{URL: rel.DownloadURL, InfoHash: rel.InfoHash, Type: rel.DownloadType, Title: rel.Title}, nil
	}
	if c.def.Download == nil || len(c.def.Download.Selectors) == 0 {
		return &DownloadRef{URL: rel.DownloadURL, InfoHash: rel.InfoHash, Type: rel.DownloadType, Title: rel.Title}, nil
	}

	headers := map[string]string{"Referer": c.baseURL + "/"}
	if cookie := c.cookieHeader(); cookie != "" {
		headers["Cookie"] = cookie
	}
	headers = c.withSession(headers)

	resp, err := c.exec.Do(ctx, httpclient.Request{
		URL:       rel.DownloadURL,
		Header:    headers,
		Allowed:   c.allowed,
		RateKey:   c.rateKey(),
		RateLimit: time.Duration(c.limitMS) * time.Millisecond,
		Proxy:     c.proxy,
	})
	if err != nil {
		return nil, translateHTTPError(err)
	}

	for _, sel := range c.def.Download.Selectors {
		value := extractFragment(resp.Body, sel.Selector, sel.Attribute)
		if value == "" {
			continue
		}
		if filtered, ferr := cardigann.ApplyFilters(value, sel.Filters); ferr == nil {
			value = filtered
		}
		if strings.HasPrefix(value, "magnet:") {
			return &DownloadRef{URL: value, Type: "magnet", Title: rel.Title}, nil
		}
		return &DownloadRef{URL: absoluteURL(c.baseURL, value), Type: "torrent", Title: rel.Title}, nil
	}

	// Some definitions expose a plain .torrent URL on the row itself.
	if rel.DownloadURL != "" {
		return &DownloadRef{URL: absoluteURL(c.baseURL, rel.DownloadURL), Type: "torrent", Title: rel.Title}, nil
	}
	return nil, NewError(KindParse, "无法从详情页取得下载链接", "download.selectors", nil)
}

// withSession applies the manual browser session (cookies + UA) to headers.
func (c *CardigannConnector) withSession(headers map[string]string) map[string]string {
	if c.session.Cookies == "" && c.session.UserAgent == "" {
		if headers["User-Agent"] == "" {
			headers["User-Agent"] = userAgent()
		}
		return headers
	}
	c.session.Apply(headers)
	return headers
}

func (c *CardigannConnector) setCookies(cookies map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range cookies {
		c.cookies[k] = v
	}
}

func (c *CardigannConnector) snapshotCookies() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		out[k] = v
	}
	return out
}

func (c *CardigannConnector) cookieHeader() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	parts := make([]string, 0, len(c.cookies))
	for _, key := range sortedKeys(c.cookies) {
		parts = append(parts, key+"="+c.cookies[key])
	}
	return strings.Join(parts, "; ")
}

func userAgent() string {
	return "BujicMovie/1.0 (+search)"
}

// needsRelogin reports whether a failure could be an expired session. A 403 is
// ambiguous: public sites use it for bot protection, but session-based sites
// also use it for an expired cookie. When the definition logs in, retry once.
func needsRelogin(err error) bool {
	var hErr *httpclient.Error
	if !asHTTPError(err, &hErr) {
		return false
	}
	return hErr.Kind == httpclient.KindAuth || (hErr.Kind == httpclient.KindBlocked && hErr.Status == http.StatusForbidden)
}

func asError(err error, target **Error) bool {
	for err != nil {
		if c, ok := err.(*Error); ok {
			*target = c
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

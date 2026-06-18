package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

type Client struct {
	apiKey     string
	baseURL    string
	language   string
	httpClient *http.Client
	limiter    *rate.Limiter
}

func NewClient(apiKey, baseURL, language string) *Client {
	if baseURL == "" {
		baseURL = "https://api.themoviedb.org/3"
	}
	if language == "" {
		language = "zh-CN"
	}
	return &Client{
		apiKey:   apiKey,
		baseURL:  baseURL,
		language: language,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		// 4 requests per second limit, burst of 10
		limiter: rate.NewLimiter(rate.Limit(4), 10),
	}
}

func (c *Client) SetAPIKey(apiKey string) {
	c.apiKey = apiKey
}

func maskAPIKey(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return urlStr
	}
	q := u.Query()
	if q.Get("api_key") != "" {
		q.Set("api_key", "******")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Client) logRequest(urlStr string, statusCode int, duration time.Duration, respBody []byte, reqErr error) {
	logFilePath := "data/logs/tmdb_api.log"
	dir := filepath.Dir(logFilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	maskedURL := maskAPIKey(urlStr)

	var logLine string
	if reqErr != nil {
		logLine = fmt.Sprintf("[%s] GET %s | DURATION: %v | ERROR: %v\n", timestamp, maskedURL, duration, reqErr)
	} else {
		bodySnippet := string(respBody)
		if len(bodySnippet) > 200 {
			bodySnippet = bodySnippet[:200] + "..."
		}
		bodySnippet = strings.ReplaceAll(bodySnippet, "\n", " ")
		bodySnippet = strings.ReplaceAll(bodySnippet, "\r", "")

		logLine = fmt.Sprintf("[%s] GET %s | STATUS: %d | DURATION: %v | RESPONSE: %s\n",
			timestamp, maskedURL, statusCode, duration, bodySnippet)
	}

	_, _ = f.WriteString(logLine)
}

func (c *Client) get(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("api_key", c.apiKey)
	q.Set("language", c.language)
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		// Wait for rate limiter
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			c.logRequest(u.String(), 0, 0, nil, err)
			return nil, err
		}

		startTime := time.Now()
		resp, err := c.httpClient.Do(req)
		duration := time.Since(startTime)
		if err != nil {
			c.logRequest(u.String(), 0, duration, nil, err)
			lastErr = err
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			c.logRequest(u.String(), resp.StatusCode, duration, nil, err)
			lastErr = err
			continue
		}

		c.logRequest(u.String(), resp.StatusCode, duration, body, nil)

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("tmdb api rate limited: status %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("tmdb api error: status %d, body %s", resp.StatusCode, string(body))
		}

		return body, nil
	}

	return nil, fmt.Errorf("tmdb api request failed after 3 attempts, last error: %w", lastErr)
}

// SearchMovie searches for movies by title and optional year
func (c *Client) SearchMovie(ctx context.Context, query string, year int) ([]SearchMovieResult, error) {
	params := map[string]string{
		"query": query,
	}
	if year > 0 {
		params["year"] = strconv.Itoa(year)
	}

	body, err := c.get(ctx, "/search/movie", params)
	if err != nil {
		return nil, err
	}

	var resp SearchMovieResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// SearchTV searches for TV shows by name and optional year
func (c *Client) SearchTV(ctx context.Context, query string, year int) ([]SearchTVResult, error) {
	params := map[string]string{
		"query": query,
	}
	if year > 0 {
		params["first_air_date_year"] = strconv.Itoa(year)
	}

	body, err := c.get(ctx, "/search/tv", params)
	if err != nil {
		return nil, err
	}

	var resp SearchTVResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// GetMovieDetail retrieves detailed metadata for a movie
func (c *Client) GetMovieDetail(ctx context.Context, id int) (*MovieDetail, error) {
	body, err := c.get(ctx, fmt.Sprintf("/movie/%d", id), nil)
	if err != nil {
		return nil, err
	}

	var detail MovieDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// GetTVDetail retrieves detailed metadata for a TV show
func (c *Client) GetTVDetail(ctx context.Context, id int) (*TVDetail, error) {
	body, err := c.get(ctx, fmt.Sprintf("/tv/%d", id), nil)
	if err != nil {
		return nil, err
	}

	var detail TVDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// GetMovieCredits retrieves the cast & crew for a movie
func (c *Client) GetMovieCredits(ctx context.Context, id int) (*CreditsResponse, error) {
	body, err := c.get(ctx, fmt.Sprintf("/movie/%d/credits", id), nil)
	if err != nil {
		return nil, err
	}

	var credits CreditsResponse
	if err := json.Unmarshal(body, &credits); err != nil {
		return nil, err
	}
	return &credits, nil
}

// GetTVCredits retrieves the cast & crew for a TV show
func (c *Client) GetTVCredits(ctx context.Context, id int) (*CreditsResponse, error) {
	body, err := c.get(ctx, fmt.Sprintf("/tv/%d/credits", id), nil)
	if err != nil {
		return nil, err
	}

	var credits CreditsResponse
	if err := json.Unmarshal(body, &credits); err != nil {
		return nil, err
	}
	return &credits, nil
}

// GetTVSeasonDetail retrieves episode list and metadata for a specific season
func (c *Client) GetTVSeasonDetail(ctx context.Context, tvID, seasonNumber int) (*TVSeasonDetail, error) {
	body, err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", tvID, seasonNumber), nil)
	if err != nil {
		return nil, err
	}

	var detail TVSeasonDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// GetMovieImages retrieves images for a movie
func (c *Client) GetMovieImages(ctx context.Context, id int) (*ImagesResponse, error) {
	// Images usually don't need language restriction to get all backdrops
	params := map[string]string{
		"include_image_language": "en,zh,null",
	}
	body, err := c.get(ctx, fmt.Sprintf("/movie/%d/images", id), params)
	if err != nil {
		return nil, err
	}

	var resp ImagesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTVImages retrieves images for a TV show
func (c *Client) GetTVImages(ctx context.Context, id int) (*ImagesResponse, error) {
	params := map[string]string{
		"include_image_language": "en,zh,null",
	}
	body, err := c.get(ctx, fmt.Sprintf("/tv/%d/images", id), params)
	if err != nil {
		return nil, err
	}

	var resp ImagesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTVSeasonImages retrieves images for a TV season
func (c *Client) GetTVSeasonImages(ctx context.Context, tvID, seasonNumber int) (*ImagesResponse, error) {
	params := map[string]string{
		"include_image_language": "en,zh,null",
	}
	body, err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d/images", tvID, seasonNumber), params)
	if err != nil {
		return nil, err
	}

	var resp ImagesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

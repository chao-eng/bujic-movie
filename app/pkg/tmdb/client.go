package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

func (c *Client) get(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	// Wait for rate limiter
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

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

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tmdb api error: status %d, body %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
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

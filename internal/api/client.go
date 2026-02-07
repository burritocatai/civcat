package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	baseURL        = "https://civitai.com/api/v1"
	defaultTimeout = 30 * time.Second

	// Conservative rate limiting: 2 requests/second, burst of 5.
	rateLimit  = 2
	burstLimit = 5
)

type Client struct {
	httpClient *http.Client
	apiKey     string

	// Token bucket rate limiter
	mu        sync.Mutex
	tokens    int
	lastFill  time.Time
}

func NewClient(apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		apiKey:     apiKey,
		tokens:     burstLimit,
		lastFill:   time.Now(),
	}
}

// waitForToken blocks until a rate limit token is available.
func (c *Client) waitForToken() {
	for {
		c.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(c.lastFill)
		newTokens := int(elapsed.Seconds() * rateLimit)
		if newTokens > 0 {
			c.tokens += newTokens
			if c.tokens > burstLimit {
				c.tokens = burstLimit
			}
			c.lastFill = now
		}
		if c.tokens > 0 {
			c.tokens--
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
}

func (c *Client) doRequest(reqURL string) (*http.Response, error) {
	c.waitForToken()

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("User-Agent", "civcat/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		retryAfter := resp.Header.Get("Retry-After")
		wait := 10 * time.Second
		if secs, err := strconv.Atoi(retryAfter); err == nil {
			wait = time.Duration(secs) * time.Second
		}
		time.Sleep(wait)
		return c.doRequest(reqURL)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return resp, nil
}

// SearchModels searches for models by query string and optional type filter.
func (c *Client) SearchModels(query string, modelType ModelType, page, limit int) (*ModelsResponse, error) {
	params := url.Values{}
	if query != "" {
		params.Set("query", query)
	}
	if modelType != "" {
		params.Set("types", string(modelType))
	}
	if page > 0 {
		params.Set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	reqURL := baseURL + "/models?" + params.Encode()
	resp, err := c.doRequest(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

// GetModel returns a single model by ID.
func (c *Client) GetModel(modelID int) (*Model, error) {
	reqURL := fmt.Sprintf("%s/models/%d", baseURL, modelID)
	resp, err := c.doRequest(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var model Model
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &model, nil
}

// GetModelVersion returns a specific model version.
func (c *Client) GetModelVersion(versionID int) (*ModelVersion, error) {
	reqURL := fmt.Sprintf("%s/model-versions/%d", baseURL, versionID)
	resp, err := c.doRequest(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var version ModelVersion
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &version, nil
}

// DownloadURL returns the authenticated download URL for a model version.
func (c *Client) DownloadURL(versionID int) string {
	u := fmt.Sprintf("https://civitai.com/api/download/models/%d", versionID)
	if c.apiKey != "" {
		u += "?token=" + url.QueryEscape(c.apiKey)
	}
	return u
}

// DownloadFile downloads a file and returns the response body (caller must close).
// It follows redirects and uses token auth via query param (required for S3 redirects).
func (c *Client) DownloadFile(versionID int) (*http.Response, error) {
	c.waitForToken()

	dlURL := c.DownloadURL(versionID)

	client := &http.Client{
		Timeout: 0, // no timeout for large downloads
	}

	resp, err := client.Get(dlURL)
	if err != nil {
		return nil, fmt.Errorf("downloading: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("download error %d: %s", resp.StatusCode, string(body))
	}

	return resp, nil
}

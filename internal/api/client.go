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

// SearchParams holds all optional filters for model search.
type SearchParams struct {
	Query     string
	ModelType ModelType
	Sort      string // "Highest Rated", "Most Downloaded", "Newest"
	Period    string // "AllTime", "Year", "Month", "Week", "Day"
	BaseModel string
	Limit     int
	Cursor    string
}

// SearchModels searches for models with optional filters.
func (c *Client) SearchModels(p SearchParams) (*ModelsResponse, error) {
	params := url.Values{}
	if p.Query != "" {
		params.Set("query", p.Query)
	}
	if p.ModelType != "" {
		params.Set("types", string(p.ModelType))
	}
	if p.Sort != "" {
		params.Set("sort", p.Sort)
	}
	if p.Period != "" {
		params.Set("period", p.Period)
	}
	if p.BaseModel != "" {
		params.Set("baseModels", p.BaseModel)
	}
	if p.Limit > 0 {
		params.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Cursor != "" {
		params.Set("cursor", p.Cursor)
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

// FetchPage fetches a full nextPage URL returned in response metadata.
func (c *Client) FetchPage(pageURL string) (*ModelsResponse, error) {
	resp, err := c.doRequest(pageURL)
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

// DownloadFile downloads a file and returns the response body (caller must close).
// Uses Authorization header for the initial civitai.com request, with a custom
// redirect policy that strips auth on cross-domain hops (S3 uses pre-signed URLs).
func (c *Client) DownloadFile(versionID int) (*http.Response, error) {
	c.waitForToken()

	dlURL := fmt.Sprintf("https://civitai.com/api/download/models/%d", versionID)

	req, err := http.NewRequest("GET", dlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating download request: %w", err)
	}

	req.Header.Set("User-Agent", "civcat/1.0")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	client := &http.Client{
		Timeout: 0, // no timeout for large downloads
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			// Strip Authorization header on cross-domain redirects.
			// S3/CDN pre-signed URLs have their own auth and will
			// reject requests that carry an extra Authorization header.
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
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

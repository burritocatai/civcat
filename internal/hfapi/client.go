package hfapi

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
	baseURL        = "https://huggingface.co/api"
	defaultTimeout = 30 * time.Second

	// Conservative rate limiting: 5 requests/second, burst of 10.
	rateLimit  = 5
	burstLimit = 10
)

// Client is a Hugging Face API client with rate limiting.
type Client struct {
	httpClient *http.Client
	token      string

	mu       sync.Mutex
	tokens   int
	lastFill time.Time
}

// NewClient creates a new Hugging Face API client.
func NewClient(token string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		token:      token,
		tokens:     burstLimit,
		lastFill:   time.Now(),
	}
}

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

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
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

// SearchParams holds optional filters for HF model search.
type SearchParams struct {
	Query     string
	Filter    string // pipeline tag filter, e.g. "text-to-image"
	Sort      string // "downloads", "likes", "lastModified"
	Direction string // "-1" (descending) or "1" (ascending)
	Limit     int
}

// SearchModels searches for models on Hugging Face.
func (c *Client) SearchModels(p SearchParams) ([]HFModel, error) {
	params := url.Values{}
	if p.Query != "" {
		params.Set("search", p.Query)
	}
	if p.Filter != "" {
		params.Set("filter", p.Filter)
	}
	if p.Sort != "" {
		params.Set("sort", p.Sort)
	}
	if p.Direction != "" {
		params.Set("direction", p.Direction)
	}
	if p.Limit > 0 {
		params.Set("limit", strconv.Itoa(p.Limit))
	}

	reqURL := baseURL + "/models?" + params.Encode()
	resp, err := c.doRequest(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var models []HFModel
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return models, nil
}

// GetModel fetches a single model by its repo ID (e.g. "stabilityai/stable-diffusion-xl-base-1.0").
func (c *Client) GetModel(repoID string) (*HFModel, error) {
	reqURL := baseURL + "/models/" + url.PathEscape(repoID)
	resp, err := c.doRequest(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var model HFModel
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &model, nil
}

// DownloadFile downloads a file from a HF repo and returns the response body (caller must close).
func (c *Client) DownloadFile(repoID, filename string) (*http.Response, error) {
	c.waitForToken()

	dlURL := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s",
		repoID, url.PathEscape(filename))

	req, err := http.NewRequest("GET", dlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating download request: %w", err)
	}

	req.Header.Set("User-Agent", "civcat/1.0")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	client := &http.Client{
		Timeout: 0, // no timeout for large downloads
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			// Strip Authorization header on cross-domain redirects (CDN).
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

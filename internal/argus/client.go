// Package argus is a typed HTTP/socket client for the argus daemon.
package argus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PluginVersion is the contract version iris sends with every request via
// the X-Argus-Plugin-Version header.
const PluginVersion = "1"

// DefaultBaseURL is the standard argus daemon HTTP base (used only when
// dynamic port discovery via the socket isn't available — e.g., tests).
const DefaultBaseURL = "http://127.0.0.1:7743"

// Client is a typed HTTP client for argus's REST API.
type Client struct {
	baseURLMu sync.RWMutex
	baseURL   string

	token string
	http  *http.Client
}

// New constructs a Client. baseURL is the argus REST root (no trailing
// slash). token is the scope token loaded from ~/.iris/api-token.
func New(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// BaseURL returns the configured base URL (for diagnostics).
func (c *Client) BaseURL() string {
	c.baseURLMu.RLock()
	defer c.baseURLMu.RUnlock()
	return c.baseURL
}

// SetBaseURL updates the client's HTTP base URL. Subsequent HTTP-issuing
// methods read the new value; in-flight requests already on the wire are
// unaffected.
func (c *Client) SetBaseURL(u string) {
	c.baseURLMu.Lock()
	c.baseURL = strings.TrimRight(u, "/")
	c.baseURLMu.Unlock()
}

// doJSON issues an HTTP request with JSON body (optional) and parses a
// JSON response (optional) into out.
func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) (int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("argus: marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL()+path, reqBody)
	if err != nil {
		return 0, fmt.Errorf("argus: new request: %w", err)
	}
	c.applyAuth(req)
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("argus: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, &HTTPError{
			Method:     method,
			Path:       path,
			StatusCode: resp.StatusCode,
			Body:       string(bytes.TrimSpace(errBody)),
		}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("argus: decode response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

func (c *Client) applyAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Argus-Plugin-Version", PluginVersion)
}

// Package yfin is a small, zero-dependency Go client for Yahoo Finance's
// unofficial JSON API, focused on fundamental data: valuation (P/E, P/B, PEG,
// EPS, market cap), financial health (ROE, margins, debt, cash flow), analyst
// coverage (rating, price targets), trading context (beta, 52-week range),
// company profile (sector, industry), and the earnings/dividend calendar.
//
// Yahoo's endpoints require a session cookie + crumb token since 2023.
// The client performs the handshake automatically, caches it in memory,
// and refreshes once on a 401/403 before surfacing an error.
package yfin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"
)

const (
	defaultBaseURL   = "https://query1.finance.yahoo.com"
	defaultCookieURL = "https://fc.yahoo.com"
	crumbPath        = "/v1/test/getcrumb"

	// Yahoo rejects default Go user agents; send a realistic browser one.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:124.0) Gecko/20100101 Firefox/124.0"

	requestTimeout = 10 * time.Second
	batchDelay     = 500 * time.Millisecond
)

// Client talks to Yahoo Finance. It is safe for concurrent use.
// Use New to create one.
type Client struct {
	httpClient *http.Client
	baseURL    string
	cookieURL  string
	delay      time.Duration // pause between BatchFundamentals requests

	mu    sync.Mutex
	crumb string
}

// New returns a ready-to-use client with a cookie jar and 10s request timeout.
func New() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		httpClient: &http.Client{Timeout: requestTimeout, Jar: jar},
		baseURL:    defaultBaseURL,
		cookieURL:  defaultCookieURL,
		delay:      batchDelay,
	}
}

// NSE returns the Yahoo Finance symbol for an NSE-listed stock ("INFY" → "INFY.NS").
func NSE(symbol string) string { return symbol + ".NS" }

// APIError is returned when Yahoo responds with a non-2xx status or an
// error payload.
type APIError struct {
	StatusCode  int
	Code        string
	Description string
}

func (e *APIError) Error() string {
	if e.Code != "" || e.Description != "" {
		return fmt.Sprintf("yfin: yahoo api error (status %d): %s: %s", e.StatusCode, e.Code, e.Description)
	}
	return fmt.Sprintf("yfin: yahoo api error: status %d", e.StatusCode)
}

// ensureCrumb returns the cached crumb, performing the cookie+crumb
// handshake on first use. force discards the cache and re-handshakes.
func (c *Client) ensureCrumb(force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.crumb != "" && !force {
		return c.crumb, nil
	}

	// Step 1: GET fc.yahoo.com — the response is a 404, that's expected;
	// we only need the Set-Cookie headers, which the jar captures.
	req, err := http.NewRequest(http.MethodGet, c.cookieURL, nil)
	if err != nil {
		return "", fmt.Errorf("yfin: build cookie request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("yfin: fetch session cookie: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// Step 2: exchange the cookie for a crumb.
	req, err = http.NewRequest(http.MethodGet, c.baseURL+crumbPath, nil)
	if err != nil {
		return "", fmt.Errorf("yfin: build crumb request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err = c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("yfin: fetch crumb: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{StatusCode: resp.StatusCode, Description: "crumb request rejected"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	if err != nil {
		return "", fmt.Errorf("yfin: read crumb: %w", err)
	}
	if len(body) == 0 {
		return "", fmt.Errorf("yfin: empty crumb returned")
	}
	c.crumb = string(body)
	return c.crumb, nil
}

// getJSON performs an authenticated GET against the API, decoding the JSON
// response into v. On a 401/403 it refreshes the cookie+crumb once and
// retries; a second failure is surfaced.
func (c *Client) getJSON(path string, query url.Values, v any) error {
	force := false
	for attempt := 0; ; attempt++ {
		crumb, err := c.ensureCrumb(force)
		if err != nil {
			return err
		}
		q := url.Values{}
		for k, vals := range query {
			q[k] = vals
		}
		q.Set("crumb", crumb)

		req, err := http.NewRequest(http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
		if err != nil {
			return fmt.Errorf("yfin: build request: %w", err)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("yfin: request %s: %w", path, err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("yfin: read response: %w", err)
		}

		if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && attempt == 0 {
			force = true
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			apiErr := &APIError{StatusCode: resp.StatusCode}
			// Error payloads look like {"quoteSummary":{"error":{"code":...,"description":...}}};
			// surface the detail when present.
			var envelope map[string]struct {
				Error *struct {
					Code        string `json:"code"`
					Description string `json:"description"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &envelope) == nil {
				for _, section := range envelope {
					if section.Error != nil {
						apiErr.Code = section.Error.Code
						apiErr.Description = section.Error.Description
					}
				}
			}
			return apiErr
		}
		if err := json.Unmarshal(body, v); err != nil {
			return fmt.Errorf("yfin: decode response: %w", err)
		}
		return nil
	}
}

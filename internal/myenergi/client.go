package myenergi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/icholy/digest"
)

// Client communicates with the myenergi API.
type Client struct {
	hubSerial string
	apiKey    string
	http      *http.Client
	baseURL   string
	mu        sync.RWMutex
	limiter   <-chan time.Time
	logger    *slog.Logger
}

// NewClient creates a myenergi API client.
// It does NOT discover the server yet — call Discover() first.
func NewClient(hubSerial, apiKey string, rateLimit time.Duration, logger *slog.Logger) *Client {
	transport := &digest.Transport{
		Username: hubSerial,
		Password: apiKey,
	}

	return &Client{
		hubSerial: hubSerial,
		apiKey:    apiKey,
		http: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		limiter: time.Tick(rateLimit),
		logger:  logger,
	}
}

// Discover queries the director to find the assigned server.
func (c *Client) Discover(ctx context.Context) error {
	c.logger.Info("discovering myenergi server")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DirectorURL, nil)
	if err != nil {
		return fmt.Errorf("creating discovery request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("discovery request failed: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	asn := resp.Header.Get("X_MYENERGI-asn")
	if asn == "" {
		return fmt.Errorf("discovery response missing X_MYENERGI-asn header (status %d)", resp.StatusCode)
	}

	c.mu.Lock()
	c.baseURL = "https://" + asn
	c.mu.Unlock()

	c.logger.Info("discovered server", "url", c.baseURL)
	return nil
}

// BaseURL returns the currently discovered server URL.
func (c *Client) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// ZappiStatus fetches real-time status for all Zappi devices.
func (c *Client) ZappiStatus(ctx context.Context) ([]ZappiStatus, error) {
	base := c.BaseURL()
	if base == "" {
		return nil, fmt.Errorf("server not discovered, call Discover() first")
	}

	var resp ZappiStatusResponse
	if err := c.get(ctx, StatusURL(base), &resp); err != nil {
		return nil, fmt.Errorf("fetching zappi status: %w", err)
	}
	return resp.Zappi, nil
}

// ZappiDayMinute fetches per-minute historical data for a specific date and serial.
func (c *Client) ZappiDayMinute(ctx context.Context, serial string, date time.Time) ([]MinuteRecord, error) {
	base := c.BaseURL()
	if base == "" {
		return nil, fmt.Errorf("server not discovered, call Discover() first")
	}

	url := DayMinuteURL(base, serial, date)

	body, err := c.getRaw(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetching day minute data: %w", err)
	}

	// The response is a JSON object with a key like "U12345678" containing the array.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing day minute response: %w", err)
	}

	key := "U" + serial
	data, ok := raw[key]
	if !ok {
		// Try without prefix — some firmware versions use different keys
		for k, v := range raw {
			if k != "status" {
				data = v
				ok = true
				break
			}
		}
	}

	if !ok || len(data) == 0 {
		return nil, nil // No data for this date
	}

	var records []MinuteRecord
	if err := json.Unmarshal(data, &records); err != nil {
		// API returns a string (e.g., error message) instead of an array when no data exists
		return nil, nil
	}

	return records, nil
}

// get performs a rate-limited GET request and decodes JSON into dest.
func (c *Client) get(ctx context.Context, url string, dest any) error {
	body, err := c.getRaw(ctx, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dest)
}

// getRaw performs a rate-limited GET request and returns the raw body.
func (c *Client) getRaw(ctx context.Context, url string) ([]byte, error) {
	// Rate limit
	select {
	case <-c.limiter:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	c.logger.Debug("GET", "url", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

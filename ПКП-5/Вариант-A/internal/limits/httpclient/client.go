package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"example.com/orderflow/internal/limits"
	"example.com/orderflow/internal/platform/resilience"
)

type Client struct {
	baseURL  *url.URL
	http     *http.Client
	retry    resilience.Retry
	bulkhead *resilience.Bulkhead
}

type request struct {
	Subject string `json:"subject"`
	Limit   string `json:"limit"`
}
type response struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

func New(baseURL string, httpClient *http.Client, retry resilience.Retry, bulkhead *resilience.Bulkhead) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("parse limits URL: %w", err)
	}
	if httpClient == nil {
		return nil, errors.New("http client is required")
	}
	if bulkhead == nil {
		return nil, errors.New("bulkhead is required")
	}
	return &Client{baseURL: parsed, http: httpClient, retry: retry, bulkhead: bulkhead}, nil
}

func (c *Client) Check(ctx context.Context, check limits.Check) error {
	if check.OperationID == "" || check.Subject == "" || check.Limit == "" {
		return errors.New("operation ID, subject and limit are required")
	}
	body, err := json.Marshal(request{Subject: check.Subject, Limit: check.Limit})
	if err != nil {
		return fmt.Errorf("encode limits request: %w", err)
	}
	err = c.bulkhead.Do(ctx, func(ctx context.Context) error {
		return c.retry.Do(ctx, func(err error) bool { return errors.Is(err, limits.ErrTransient) }, func(ctx context.Context) error {
			return c.call(ctx, check.OperationID, body)
		})
	})
	if errors.Is(err, resilience.ErrBusy) {
		return limits.ErrBusy
	}
	return err
}

func (c *Client) call(ctx context.Context, operationID string, body []byte) error {
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: "/v1/limits/check"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create limits request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "limit:"+operationID)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: send request: %v", limits.ErrTransient, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("%w: status %d", limits.ErrTransient, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("limits returned status %d", resp.StatusCode)
	}
	var result response
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return fmt.Errorf("decode limits response: %w", err)
	}
	if !result.Allowed {
		reason := strings.TrimSpace(result.Reason)
		if reason == "" {
			reason = limits.ErrDenied.Error()
		}
		return fmt.Errorf("%w: %s", limits.ErrDenied, reason)
	}
	return nil
}

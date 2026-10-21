package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const (
	// The most of an error body read, and the longest reason kept from it in an APIError's Detail.
	maxErrorBody   = 64 << 10
	maxErrorDetail = 512
	// The most of an answer left after decoding that is read so its connection is reused;
	// a longer one's connection is closed.
	maxDrainedBody = 64 << 10
)

var defaultHTTPClient = &http.Client{Timeout: 60 * time.Second}

// APIError is a non-2xx answer from the Stream Security REST API.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	// Detail is the reason given: ms_accounts' "detail", the front gate's "error", or the body itself.
	Detail string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s %s returned %d %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode))
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// AccountsPath is the front gate's REST path to an ms_accounts /accounts route,
// /api/accounts/accounts/<segments>, with each segment escaped.
func AccountsPath(segments ...string) string {
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		escaped[i] = url.PathEscape(segment)
	}
	return "/api/accounts/accounts/" + strings.Join(escaped, "/")
}

// DoRESTRequest calls https://<host><path> with the credentials DoRequest sends: the
// api token or login token as the bearer token, and the workspace as the customer header.
// A non-nil body is sent as JSON, and a 2xx answer is decoded into a non-nil out.
// Any other answer is returned as an *APIError.
func (c *Client) DoRESTRequest(ctx context.Context, method, path string, body, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding the %s %s body: %w", method, path, err)
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("https://%s%s", c.Host, path), reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.Workspace != "" {
		req.Header.Set("customer", c.Workspace)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	tflog.Debug(ctx, "Stream Security REST call", map[string]interface{}{"method": method, "path": path, "status": resp.StatusCode})

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Only the reason is kept, so the error body is capped; an answer is not.
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		if err != nil {
			return fmt.Errorf("reading the %s %s answer: %w", method, path, err)
		}
		return &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Detail: errorDetail(respBody)}
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decoding the %s %s answer: %w", method, path, err)
		}
	}
	// A body closed unread closes its connection, so the next call would open another.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainedBody))
	return nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return defaultHTTPClient
}

func errorDetail(body []byte) string {
	text := strings.TrimSpace(string(body))
	var parsed struct {
		Detail json.RawMessage `json:"detail"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		for _, raw := range []json.RawMessage{parsed.Detail, parsed.Error} {
			if len(raw) == 0 || string(raw) == "null" {
				continue
			}
			// A string, or as is: FastAPI's validation errors are a list.
			var reason string
			if json.Unmarshal(raw, &reason) != nil {
				reason = string(raw)
			}
			text = reason
			break
		}
	}
	if len(text) > maxErrorDetail {
		cut := maxErrorDetail
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "..."
	}
	return text
}

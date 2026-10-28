package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type capturedRequest struct {
	method  string
	uri     string
	headers http.Header
	body    []byte
}

type capturedRequests struct {
	mu       sync.Mutex
	requests []capturedRequest
}

func (c *capturedRequests) get(t *testing.T, i int) capturedRequest {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if i >= len(c.requests) {
		t.Fatalf("got %d requests, want more than %d", len(c.requests), i)
	}
	return c.requests[i]
}

func (c *capturedRequests) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func newRESTTestServer(t *testing.T, status int, response string) (*httptest.Server, *capturedRequests) {
	t.Helper()
	captured := &capturedRequests{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %s", err)
		}
		captured.mu.Lock()
		captured.requests = append(captured.requests, capturedRequest{method: r.Method, uri: r.RequestURI, headers: r.Header.Clone(), body: body})
		captured.mu.Unlock()
		answer := response
		if strings.HasPrefix(r.RequestURI, "/api/") && status >= 200 && status <= 299 {
			answer = `{"status":"OK","data":` + answer + `}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func restTestClient(srv *httptest.Server, token, workspace string) *Client {
	return &Client{
		Host:       srv.Listener.Addr().String(),
		Token:      token,
		Workspace:  workspace,
		HTTPClient: srv.Client(),
	}
}

func TestAccountsPathEscapesEachSegment(t *testing.T) {
	got := AccountsPath("a/../b c", "scanner-hub")
	want := "/api/accounts/accounts/a%2F..%2Fb%20c/scanner-hub"
	if got != want {
		t.Fatalf("AccountsPath = %q, want %q", got, want)
	}
}

func TestDoRESTRequestSendsMethodPathHeadersAndJSONBody(t *testing.T) {
	srv, captured := newRESTTestServer(t, http.StatusOK, `{"region":"us-east-1","status":"deployed"}`)
	c := restTestClient(srv, "tok-123", "workspace-1")

	var out struct {
		Region string `json:"region"`
		Status string `json:"status"`
	}
	err := c.DoRESTRequest(context.Background(), http.MethodPut, AccountsPath("acc-1", "scanner-terraform-acknowledge"),
		map[string]string{"region": "us-east-1", "status": "deployed"}, &out)
	if err != nil {
		t.Fatalf("DoRESTRequest: %s", err)
	}
	if n := captured.count(); n != 1 {
		t.Fatalf("got %d requests, want 1", n)
	}
	req := captured.get(t, 0)
	if req.method != http.MethodPut {
		t.Errorf("method = %s, want PUT", req.method)
	}
	if req.uri != "/api/accounts/accounts/acc-1/scanner-terraform-acknowledge" {
		t.Errorf("uri = %s", req.uri)
	}
	if got := req.headers.Get("Authorization"); got != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok-123")
	}
	if got := req.headers.Get("customer"); got != "workspace-1" {
		t.Errorf("customer = %q, want %q", got, "workspace-1")
	}
	if got := req.headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var sent map[string]string
	if err := json.Unmarshal(req.body, &sent); err != nil {
		t.Fatalf("body is not JSON: %s (%q)", err, req.body)
	}
	if sent["region"] != "us-east-1" || sent["status"] != "deployed" || len(sent) != 2 {
		t.Errorf("body = %v", sent)
	}
	if out.Region != "us-east-1" || out.Status != "deployed" {
		t.Errorf("decoded response = %+v", out)
	}
}

func TestDoRESTRequestGetSendsNoBodyAndKeepsQuery(t *testing.T) {
	srv, captured := newRESTTestServer(t, http.StatusOK, `{}`)
	c := restTestClient(srv, "tok", "ws")

	if err := c.DoRESTRequest(context.Background(), http.MethodGet, AccountsPath("scanner-coverage")+"?cloud=AWS", nil, nil); err != nil {
		t.Fatalf("DoRESTRequest: %s", err)
	}
	req := captured.get(t, 0)
	if req.method != http.MethodGet || req.uri != "/api/accounts/accounts/scanner-coverage?cloud=AWS" {
		t.Errorf("request = %s %s", req.method, req.uri)
	}
	if len(req.body) != 0 {
		t.Errorf("GET sent a body: %q", req.body)
	}
	if got := req.headers.Get("Content-Type"); got != "" {
		t.Errorf("GET sent Content-Type %q", got)
	}
}

// A provider configured with an api_token and no workspace_id sends no customer
// header, like the GraphQL path; one without a token sends no Authorization.
func TestDoRESTRequestOmitsEmptyCredentials(t *testing.T) {
	srv, captured := newRESTTestServer(t, http.StatusOK, `{}`)

	if err := restTestClient(srv, "tok", "").DoRESTRequest(context.Background(), http.MethodGet, "/api/x", nil, nil); err != nil {
		t.Fatalf("DoRESTRequest: %s", err)
	}
	if _, ok := captured.get(t, 0).headers["Customer"]; ok {
		t.Errorf("sent a customer header without a workspace")
	}
	if err := restTestClient(srv, "", "ws").DoRESTRequest(context.Background(), http.MethodGet, "/api/x", nil, nil); err != nil {
		t.Fatalf("DoRESTRequest: %s", err)
	}
	if _, ok := captured.get(t, 1).headers["Authorization"]; ok {
		t.Errorf("sent an Authorization header without a token")
	}
}

func TestDoRESTRequestNon2xxIsAPIErrorWithDetail(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantDetail string
	}{
		{"ms_accounts detail", http.StatusConflict, `{"detail":"Account x's scanner already scans all AWS accounts"}`, "Account x's scanner already scans all AWS accounts"},
		// The front gate answers a failed proxied call with {"error": ...}.
		{"front gate error", http.StatusConflict, `{"error":"Request failed with status code 409"}`, "Request failed with status code 409"},
		{"validation detail list", http.StatusUnprocessableEntity, `{"detail":[{"loc":["body"],"msg":"field required"}]}`, `[{"loc":["body"],"msg":"field required"}]`},
		{"plain text", http.StatusBadGateway, "upstream down\n", "upstream down"},
		{"empty", http.StatusNotFound, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newRESTTestServer(t, tc.status, tc.body)
			err := restTestClient(srv, "tok", "ws").DoRESTRequest(context.Background(), http.MethodGet, "/api/accounts/accounts/a/b", nil, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v (%T), want *APIError", err, err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.Detail != tc.wantDetail {
				t.Errorf("Detail = %q, want %q", apiErr.Detail, tc.wantDetail)
			}
			msg := err.Error()
			if !strings.Contains(msg, "GET /api/accounts/accounts/a/b") || !strings.Contains(msg, http.StatusText(tc.status)) {
				t.Errorf("Error() = %q, want the request and the status in it", msg)
			}
			if tc.wantDetail != "" && !strings.Contains(msg, tc.wantDetail) {
				t.Errorf("Error() = %q, want the detail in it", msg)
			}
		})
	}
}

func TestDoRESTRequestLongErrorBodyIsTruncated(t *testing.T) {
	srv, _ := newRESTTestServer(t, http.StatusInternalServerError, strings.Repeat("x", 5000))
	err := restTestClient(srv, "tok", "ws").DoRESTRequest(context.Background(), http.MethodGet, "/api/x", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if len(apiErr.Detail) > 600 {
		t.Errorf("Detail is %d bytes, want it truncated", len(apiErr.Detail))
	}
}

func TestDoRESTRequestTruncatesOnARuneBoundary(t *testing.T) {
	// One ASCII byte first, so the cut falls inside a two-byte rune.
	srv, _ := newRESTTestServer(t, http.StatusInternalServerError, "x"+strings.Repeat("é", 1000))
	err := restTestClient(srv, "tok", "ws").DoRESTRequest(context.Background(), http.MethodGet, "/api/x", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if !utf8.ValidString(apiErr.Detail) || !strings.HasSuffix(apiErr.Detail, "...") || len(apiErr.Detail) > 600 {
		t.Errorf("Detail = %q (%d bytes), want valid UTF-8 cut short with ...", apiErr.Detail, len(apiErr.Detail))
	}
}

// A Client built without NewClient, as in tests, runs GraphQL over its HTTPClient.
func TestDoRequestUsesHTTPClientWithoutNewClient(t *testing.T) {
	srv, captured := newRESTTestServer(t, http.StatusOK, `{"data":{"accounts":[{"_id":"a1","account_type":"AWS"}]}}`)

	data, err := restTestClient(srv, "tok", "ws").DoRequest(`query { accounts { _id account_type } }`, nil)
	if err != nil {
		t.Fatalf("DoRequest: %s", err)
	}
	req := captured.get(t, 0)
	if req.method != http.MethodPost || req.uri != "/graphql" || req.headers.Get("Authorization") != "Bearer tok" || req.headers.Get("customer") != "ws" {
		t.Errorf("request = %s %s %v", req.method, req.uri, req.headers)
	}
	accounts, _ := data["accounts"].([]interface{})
	if len(accounts) != 1 {
		t.Errorf("data = %v", data)
	}
}

// A big workspace's scanner coverage is megabytes; a cap on answers would break every plan.
func TestDoRESTRequestDecodesALargeAnswer(t *testing.T) {
	padding := strings.Repeat("x", 3<<20)
	srv, _ := newRESTTestServer(t, http.StatusOK, `{"padding":"`+padding+`","value":"ok"}`)
	var out struct {
		Padding string `json:"padding"`
		Value   string `json:"value"`
	}
	if err := restTestClient(srv, "tok", "ws").DoRESTRequest(context.Background(), http.MethodGet, "/api/x", nil, &out); err != nil {
		t.Fatalf("DoRESTRequest: %s", err)
	}
	if out.Value != "ok" || len(out.Padding) != len(padding) {
		t.Errorf("decoded value %q and %d bytes of padding, want ok and %d", out.Value, len(out.Padding), len(padding))
	}
}

func TestDoRESTRequestUndecodableSuccessIsAnError(t *testing.T) {
	srv, _ := newRESTTestServer(t, http.StatusOK, `not json`)
	var out map[string]interface{}
	if err := restTestClient(srv, "tok", "ws").DoRESTRequest(context.Background(), http.MethodGet, "/api/x", nil, &out); err == nil {
		t.Fatal("want an error for a 200 that isn't JSON")
	}
}

// A hub rollout makes a call per account: each must not pay for a new TLS connection.
func TestDoRESTRequestReusesTheConnection(t *testing.T) {
	srv, _ := newRESTTestServer(t, http.StatusOK, `{"region":"us-east-1","status":"deployed"}`)
	c := restTestClient(srv, "tok", "ws")
	var reused []bool
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { reused = append(reused, info.Reused) },
	})
	var out map[string]interface{}
	for _, o := range []interface{}{nil, &out, nil} {
		if err := c.DoRESTRequest(ctx, http.MethodPut, "/api/x", map[string]string{"status": "deployed"}, o); err != nil {
			t.Fatalf("DoRESTRequest: %s", err)
		}
	}
	if want := []bool{false, true, true}; !reflect.DeepEqual(reused, want) {
		t.Errorf("connection reused = %v, want %v", reused, want)
	}
}

func TestDoRequestContextStopsWithItsContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := restTestClient(srv, "tok", "ws").DoRequestContext(ctx, `query { accounts { _id } }`, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's deadline", err)
	}
}

func TestNewClientWithAPITokenUsesItForREST(t *testing.T) {
	host, token, workspace := "example.invalid", "api-tok", "ws-9"
	empty := ""
	c, err := NewClient(&host, &empty, &empty, &workspace, &token)
	if err != nil {
		t.Fatalf("NewClient: %s", err)
	}
	if c.Token != token || c.Workspace != workspace || c.Host != host {
		t.Errorf("client = %+v", c)
	}
}

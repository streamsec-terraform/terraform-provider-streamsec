package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"terraform-provider-streamsec/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const (
	testToken     = "test-token"
	testWorkspace = "ws-test"
)

type apiCall struct {
	Method   string
	URI      string
	Auth     string
	Customer string
	Body     map[string]interface{}
	// AnyBody, on an expected call, skips comparing the body.
	AnyBody bool
}

type fakeResponse struct {
	status int
	body   string
}

// fakeAPI stands in for the front gate: it answers "METHOD URI" from routes and
// records every call. Any other call fails the test.
type fakeAPI struct {
	t      *testing.T
	routes map[string]fakeResponse
	mu     sync.Mutex
	calls  []apiCall
}

func newFakeAPI(t *testing.T, routes map[string]fakeResponse) (*fakeAPI, *client.Client) {
	t.Helper()
	api := &fakeAPI{t: t, routes: routes}
	srv := httptest.NewTLSServer(http.HandlerFunc(api.serve))
	t.Cleanup(srv.Close)
	return api, &client.Client{
		Host:       srv.Listener.Addr().String(),
		Token:      testToken,
		Workspace:  testWorkspace,
		HTTPClient: srv.Client(),
	}
}

func (a *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	call := apiCall{Method: r.Method, URI: r.RequestURI, Auth: r.Header.Get("Authorization"), Customer: r.Header.Get("customer")}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		a.t.Errorf("reading the request body: %s", err)
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &call.Body); err != nil {
			a.t.Errorf("request body is not a JSON object: %s (%q)", err, raw)
		}
	}
	a.mu.Lock()
	a.calls = append(a.calls, call)
	a.mu.Unlock()

	route, ok := a.routes[r.Method+" "+r.RequestURI]
	if !ok {
		a.t.Errorf("unexpected call %s %s", r.Method, r.RequestURI)
		route = fakeResponse{http.StatusTeapot, `{"detail":"unexpected call"}`}
	}
	body := route.body
	if strings.HasPrefix(r.RequestURI, "/api/") && route.status >= 200 && route.status <= 299 {
		body = `{"status":"OK","data":` + body + `}`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(route.status)
	_, _ = w.Write([]byte(body))
}

func (a *fakeAPI) recorded() []apiCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]apiCall(nil), a.calls...)
}

// expectCalls checks the calls made, in order, and that each carried the
// provider's token and workspace.
func (a *fakeAPI) expectCalls(want ...apiCall) {
	a.t.Helper()
	got := a.recorded()
	if len(got) != len(want) {
		a.t.Fatalf("got %d calls %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i].Method != want[i].Method || got[i].URI != want[i].URI {
			a.t.Errorf("call %d = %s %s, want %s %s", i, got[i].Method, got[i].URI, want[i].Method, want[i].URI)
		}
		if got[i].Auth != "Bearer "+testToken {
			a.t.Errorf("call %d Authorization = %q, want the provider's token", i, got[i].Auth)
		}
		if got[i].Customer != testWorkspace {
			a.t.Errorf("call %d customer = %q, want the provider's workspace", i, got[i].Customer)
		}
		if !want[i].AnyBody && jsonOf(a.t, got[i].Body) != jsonOf(a.t, want[i].Body) {
			a.t.Errorf("call %d body = %s, want %s", i, jsonOf(a.t, got[i].Body), jsonOf(a.t, want[i].Body))
		}
	}
}

func jsonOf(t *testing.T, v interface{}) string {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}
	return string(out)
}

func configured(t *testing.T, r resource.Resource, c *client.Client) rschema.Schema {
	t.Helper()
	ctx := context.Background()
	if rc, ok := r.(resource.ResourceWithConfigure); ok {
		resp := &resource.ConfigureResponse{}
		rc.Configure(ctx, resource.ConfigureRequest{ProviderData: c}, resp)
		requireNoDiags(t, resp.Diagnostics)
	}
	resp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, resp)
	requireNoDiags(t, resp.Diagnostics)
	return resp.Schema
}

func planOf(t *testing.T, s rschema.Schema, model interface{}) tfsdk.Plan {
	t.Helper()
	plan := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
	requireNoDiags(t, plan.Set(context.Background(), model))
	return plan
}

func stateOf(t *testing.T, s rschema.Schema, model interface{}) tfsdk.State {
	t.Helper()
	state := nullState(s)
	requireNoDiags(t, state.Set(context.Background(), model))
	return state
}

func configOf(t *testing.T, s rschema.Schema, model interface{}) tfsdk.Config {
	t.Helper()
	return tfsdk.Config{Schema: s, Raw: stateOf(t, s, model).Raw}
}

func nullState(s rschema.Schema) tfsdk.State {
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

func doCreate(t *testing.T, r resource.Resource, s rschema.Schema, plan interface{}) (tfsdk.State, diag.Diagnostics) {
	t.Helper()
	resp := &resource.CreateResponse{State: nullState(s)}
	p := planOf(t, s, plan)
	r.Create(context.Background(), resource.CreateRequest{Plan: p, Config: tfsdk.Config{Schema: s, Raw: p.Raw}}, resp)
	return resp.State, resp.Diagnostics
}

func doRead(t *testing.T, r resource.Resource, s rschema.Schema, prior interface{}) (tfsdk.State, diag.Diagnostics) {
	t.Helper()
	state := stateOf(t, s, prior)
	resp := &resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, resp)
	return resp.State, resp.Diagnostics
}

func doUpdate(t *testing.T, r resource.Resource, s rschema.Schema, prior, plan interface{}) (tfsdk.State, diag.Diagnostics) {
	t.Helper()
	state := stateOf(t, s, prior)
	p := planOf(t, s, plan)
	resp := &resource.UpdateResponse{State: state}
	r.Update(context.Background(), resource.UpdateRequest{Plan: p, State: state, Config: tfsdk.Config{Schema: s, Raw: p.Raw}}, resp)
	return resp.State, resp.Diagnostics
}

func doDelete(t *testing.T, r resource.Resource, s rschema.Schema, prior interface{}) diag.Diagnostics {
	t.Helper()
	state := stateOf(t, s, prior)
	resp := &resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, resp)
	return resp.Diagnostics
}

func requireNoDiags(t *testing.T, diags diag.Diagnostics) {
	t.Helper()
	if len(diags) > 0 {
		t.Fatalf("unexpected diagnostics: %s", diagsText(diags))
	}
}

// requireError checks there is an error whose text contains every one of want.
func requireError(t *testing.T, diags diag.Diagnostics, want ...string) {
	t.Helper()
	if !diags.HasError() {
		t.Fatalf("want an error, got diagnostics: %s", diagsText(diags))
	}
	text := diagsText(diags.Errors())
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("error %q does not mention %q", text, w)
		}
	}
}

func diagsText(diags diag.Diagnostics) string {
	parts := make([]string, 0, len(diags))
	for _, d := range diags {
		parts = append(parts, fmt.Sprintf("[%s] %s: %s", d.Severity(), d.Summary(), d.Detail()))
	}
	return strings.Join(parts, "; ")
}

func getState(t *testing.T, state tfsdk.State, target interface{}) {
	t.Helper()
	requireNoDiags(t, state.Get(context.Background(), target))
}

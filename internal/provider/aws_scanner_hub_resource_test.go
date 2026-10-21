package provider

import (
	"context"
	"net/http"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	hubPath      = "/api/accounts/accounts/hub-1/scanner-hub"
	coveragePath = "/api/accounts/accounts/scanner-coverage?cloud=AWS"
)

func stringSet(values ...string) types.Set {
	elements := make([]attr.Value, len(values))
	for i, v := range values {
		elements[i] = types.StringValue(v)
	}
	return types.SetValueMust(types.StringType, elements)
}

func setStrings(t *testing.T, set types.Set) []string {
	t.Helper()
	var out []string
	requireNoDiags(t, set.ElementsAs(context.Background(), &out, false))
	sort.Strings(out)
	return out
}

func hubModel(coverage string, accountIDs types.Set) AWSScannerHubResourceModel {
	return AWSScannerHubResourceModel{
		AccountID:  types.StringValue("hub-1"),
		Coverage:   types.StringValue(coverage),
		AccountIDs: accountIDs,
	}
}

func TestScannerHubCreateSelectedPutsCoverageAndAccounts(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + hubPath: {200, `{"hubs":[]}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	state, diags := doCreate(t, r, s, hubModel("selected", stringSet("acc-b", "acc-a")))
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPut, URI: hubPath, Body: map[string]interface{}{
		"coverage": "selected", "accounts": []interface{}{"acc-a", "acc-b"},
	}})
	var got AWSScannerHubResourceModel
	getState(t, state, &got)
	if got.AccountID.ValueString() != "hub-1" || got.Coverage.ValueString() != "selected" {
		t.Errorf("state = %+v", got)
	}
	if ids := setStrings(t, got.AccountIDs); len(ids) != 2 || ids[0] != "acc-a" || ids[1] != "acc-b" {
		t.Errorf("account_ids = %v", ids)
	}
}

func TestScannerHubCreateAllSendsNoAccounts(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + hubPath: {200, `{}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	state, diags := doCreate(t, r, s, hubModel("all", types.SetNull(types.StringType)))
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPut, URI: hubPath, Body: map[string]interface{}{
		"coverage": "all", "accounts": []interface{}{},
	}})
	var got AWSScannerHubResourceModel
	getState(t, state, &got)
	if got.Coverage.ValueString() != "all" || !got.AccountIDs.IsNull() {
		t.Errorf("state = %+v", got)
	}
}

func TestScannerHubCreateConflictIsExplained(t *testing.T) {
	// Through the front gate a 409 arrives without ms_accounts' detail.
	_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + hubPath: {409, `{"error":"Request failed with status code 409"}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	state, diags := doCreate(t, r, s, hubModel("all", types.SetNull(types.StringType)))

	requireError(t, diags, "409", "Request failed with status code 409", "already scans all AWS accounts", `only one hub may cover "all"`, "already scanned by another")
	if !state.Raw.IsNull() {
		t.Errorf("a failed create left state %v", state.Raw)
	}
}

func TestScannerHubCreateShowsDetail(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + hubPath: {400, `{"detail":"Account acc-x is not a connected AWS account"}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	_, diags := doCreate(t, r, s, hubModel("selected", stringSet("acc-x")))

	requireError(t, diags, "400", "Account acc-x is not a connected AWS account")
}

func TestScannerHubUpdatePutsNewCoverage(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + hubPath: {200, `{}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	state, diags := doUpdate(t, r, s, hubModel("selected", stringSet("acc-a")), hubModel("all", types.SetValueMust(types.StringType, nil)))
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPut, URI: hubPath, Body: map[string]interface{}{
		"coverage": "all", "accounts": []interface{}{},
	}})
	var got AWSScannerHubResourceModel
	getState(t, state, &got)
	if got.Coverage.ValueString() != "all" || got.AccountIDs.IsNull() || len(got.AccountIDs.Elements()) != 0 {
		t.Errorf("state = %+v", got)
	}
}

func TestScannerHubDeleteTurnsCoverageOff(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + hubPath: {200, `{}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	requireNoDiags(t, doDelete(t, r, s, hubModel("selected", stringSet("acc-a"))))

	api.expectCalls(apiCall{Method: http.MethodPut, URI: hubPath, Body: map[string]interface{}{"coverage": "none"}})
}

func TestScannerHubDeleteFailureIsAnError(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + hubPath: {403, `{"error":"Request failed with status code 403"}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	requireError(t, doDelete(t, r, s, hubModel("all", types.SetNull(types.StringType))), "403", "access to all accounts", "trial")
}

// A hub whose account is gone from Stream, or already turned off, has nothing
// left to turn off: destroying it must not fail. Without an error the framework
// removes it from state.
func TestScannerHubDeleteOfAGoneHubWarns(t *testing.T) {
	for _, status := range []int{404, 409} {
		_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + hubPath: {status, `{"error":"gone"}`}})
		r := NewAWSScannerHubResource()
		s := configured(t, r, c)

		diags := doDelete(t, r, s, hubModel("selected", stringSet("acc-a")))

		if diags.HasError() || diags.WarningsCount() != 1 {
			t.Errorf("status %d: diagnostics = %s, want one warning", status, diagsText(diags))
		}
	}
}

func TestScannerHubCreateBadRequestNamesTheFields(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + hubPath: {400, `{"error":"Request failed with status code 400"}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	_, diags := doCreate(t, r, s, hubModel("selected", stringSet("acc-x")))

	requireError(t, diags, "400", "account_id", "account_ids")
}

const coverageBody = `{
  "hubs": [
    {"hub_account_id": "hub-2", "coverage": "all", "created_at": "2026-10-01T00:00:00Z"},
    {"hub_account_id": "hub-1", "coverage": "selected", "created_at": "2026-10-02T00:00:00Z"}
  ],
  "assignments": [
    {"account_id": "acc-a", "hub_account_id": "hub-1", "updated_at": "2026-10-02T00:00:00Z"},
    {"account_id": "acc-b", "hub_account_id": "hub-2", "updated_at": "2026-10-02T00:00:00Z"},
    {"account_id": "acc-c", "hub_account_id": "hub-1", "updated_at": "2026-10-02T00:00:00Z"},
    {"account_id": "acc-d", "hub_account_id": null, "updated_at": "2026-10-02T00:00:00Z"}
  ],
  "access_stacks": [],
  "spoke_scans": [],
  "hub_scanners": []
}`

func TestScannerHubReadMapsCoverageAndThisHubsAssignments(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"GET " + coveragePath: {200, coverageBody}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	// The prior state drifted: the hub's accounts were changed outside Terraform.
	state, diags := doRead(t, r, s, hubModel("all", stringSet("acc-z")))
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodGet, URI: coveragePath})
	var got AWSScannerHubResourceModel
	getState(t, state, &got)
	if got.AccountID.ValueString() != "hub-1" || got.Coverage.ValueString() != "selected" {
		t.Errorf("state = %+v", got)
	}
	if ids := setStrings(t, got.AccountIDs); len(ids) != 2 || ids[0] != "acc-a" || ids[1] != "acc-c" {
		t.Errorf("account_ids = %v, want this hub's assignments [acc-a acc-c]", ids)
	}
}

func TestScannerHubReadSelectedWithNoAssignmentsIsEmpty(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"GET " + coveragePath: {200,
		`{"hubs":[{"hub_account_id":"hub-1","coverage":"selected"}],"assignments":[]}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	state, diags := doRead(t, r, s, hubModel("selected", stringSet("acc-a")))
	requireNoDiags(t, diags)

	var got AWSScannerHubResourceModel
	getState(t, state, &got)
	if got.AccountIDs.IsNull() || len(got.AccountIDs.Elements()) != 0 {
		t.Errorf("account_ids = %v, want an empty set", got.AccountIDs)
	}
}

// An "all" hub scans every account, so the accounts pinned to it are not its
// account_ids: reading them would plan a change on every run.
func TestScannerHubReadAllKeepsAccountIDsEmpty(t *testing.T) {
	cases := []struct {
		name      string
		prior     types.Set
		wantNull  bool
		wantEmpty bool
	}{
		{"unset stays unset", types.SetNull(types.StringType), true, false},
		{"empty stays empty", types.SetValueMust(types.StringType, nil), false, true},
		{"left from selected clears", stringSet("acc-a"), true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, c := newFakeAPI(t, map[string]fakeResponse{"GET " + coveragePath: {200, coverageBody}})
			r := NewAWSScannerHubResource()
			s := configured(t, r, c)

			prior := hubModel("all", tc.prior)
			prior.AccountID = types.StringValue("hub-2")
			state, diags := doRead(t, r, s, prior)
			requireNoDiags(t, diags)

			var got AWSScannerHubResourceModel
			getState(t, state, &got)
			if got.Coverage.ValueString() != "all" {
				t.Errorf("coverage = %s", got.Coverage)
			}
			if got.AccountIDs.IsNull() != tc.wantNull || (tc.wantEmpty && len(got.AccountIDs.Elements()) != 0) {
				t.Errorf("account_ids = %v", got.AccountIDs)
			}
		})
	}
}

func TestScannerHubReadRemovesAHubGoneFromCoverage(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"GET " + coveragePath: {200,
		`{"hubs":[{"hub_account_id":"hub-2","coverage":"all"}],"assignments":[{"account_id":"acc-a","hub_account_id":"hub-1"}]}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	state, diags := doRead(t, r, s, hubModel("selected", stringSet("acc-a")))
	requireNoDiags(t, diags)

	if !state.Raw.IsNull() {
		t.Errorf("state = %v, want the hub removed", state.Raw)
	}
}

func TestScannerHubReadFailureIsAnError(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"GET " + coveragePath: {500, `{"error":"No authorized"}`}})
	r := NewAWSScannerHubResource()
	s := configured(t, r, c)

	_, diags := doRead(t, r, s, hubModel("selected", stringSet("acc-a")))

	requireError(t, diags, "500", "No authorized")
}

func TestScannerHubImportByAccountID(t *testing.T) {
	r := NewAWSScannerHubResource()
	s := configured(t, r, nil)

	resp := &resource.ImportStateResponse{State: nullState(s)}
	r.(resource.ResourceWithImportState).ImportState(context.Background(), resource.ImportStateRequest{ID: "hub-1"}, resp)
	requireNoDiags(t, resp.Diagnostics)

	var got AWSScannerHubResourceModel
	getState(t, resp.State, &got)
	if got.AccountID.ValueString() != "hub-1" {
		t.Errorf("imported account_id = %s", got.AccountID)
	}
}

func TestScannerHubValidateConfig(t *testing.T) {
	cases := []struct {
		name       string
		coverage   types.String
		accountIDs types.Set
		wantError  bool
	}{
		{"selected with accounts", types.StringValue("selected"), stringSet("acc-a"), false},
		{"selected with none", types.StringValue("selected"), types.SetValueMust(types.StringType, nil), false},
		{"selected unset", types.StringValue("selected"), types.SetNull(types.StringType), true},
		{"selected not yet known", types.StringValue("selected"), types.SetUnknown(types.StringType), false},
		{"all unset", types.StringValue("all"), types.SetNull(types.StringType), false},
		{"all empty", types.StringValue("all"), types.SetValueMust(types.StringType, nil), false},
		{"all with accounts", types.StringValue("all"), stringSet("acc-a"), true},
		{"coverage not yet known", types.StringUnknown(), types.SetNull(types.StringType), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewAWSScannerHubResource()
			s := configured(t, r, nil)
			model := AWSScannerHubResourceModel{AccountID: types.StringValue("hub-1"), Coverage: tc.coverage, AccountIDs: tc.accountIDs}

			resp := &resource.ValidateConfigResponse{}
			r.(resource.ResourceWithValidateConfig).ValidateConfig(context.Background(),
				resource.ValidateConfigRequest{Config: configOf(t, s, model)}, resp)

			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("diagnostics = %s, want error %v", diagsText(resp.Diagnostics), tc.wantError)
			}
			for _, d := range resp.Diagnostics.Errors() {
				withPath, ok := d.(interface{ Path() path.Path })
				if !ok || !withPath.Path().Equal(path.Root("account_ids")) {
					t.Errorf("error %q is not on account_ids", d.Summary())
				}
			}
		})
	}
}

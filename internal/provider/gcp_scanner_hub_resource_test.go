package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// GCP: the AWS resource's protocol, with the hub's cloud in the account check and the read.

const (
	gcpHubPath      = "/api/accounts/accounts/gcp-hub/scanner-hub"
	gcpCoveragePath = "/api/accounts/accounts/scanner-coverage?cloud=GCP"
)

func gcpHubModel(coverage string, accountIDs types.Set) ScannerHubResourceModel {
	return ScannerHubResourceModel{
		AccountID:  types.StringValue("gcp-hub"),
		Coverage:   types.StringValue(coverage),
		AccountIDs: accountIDs,
	}
}

func gcpCreateRoutes(put fakeResponse) map[string]fakeResponse {
	return map[string]fakeResponse{
		"POST /graphql":     accountsAnswer(`[{"_id":"gcp-hub","account_type":"GCP"},{"_id":"aws-1","account_type":"AWS"}]`),
		"PUT " + gcpHubPath: put,
	}
}

func TestGCPScannerHubCreatePutsCoverageAndProjects(t *testing.T) {
	api, c := newFakeAPI(t, gcpCreateRoutes(fakeResponse{200, `{}`}))
	r := NewGCPScannerHubResource()
	s := configured(t, r, c)

	_, diags := doCreate(t, r, s, gcpHubModel("selected", stringSet("proj-b", "proj-a")))
	requireNoDiags(t, diags)

	api.expectCalls(accountsCall, apiCall{Method: http.MethodPut, URI: gcpHubPath, Body: map[string]interface{}{
		"coverage": "selected", "accounts": []interface{}{"proj-a", "proj-b"},
	}})
}

func TestGCPScannerHubCreateRefusesAnAccountThatIsNotGCP(t *testing.T) {
	api, c := newFakeAPI(t, gcpCreateRoutes(fakeResponse{200, `{}`}))
	r := NewGCPScannerHubResource()
	s := configured(t, r, c)

	model := gcpHubModel("all", types.SetNull(types.StringType))
	model.AccountID = types.StringValue("aws-1")
	_, diags := doCreate(t, r, s, model)

	requireError(t, diags, "aws-1", "AWS", "streamsec_gcp_scanner_hub takes a GCP project")
	api.expectCalls(accountsCall)
}

func TestGCPScannerHubReadsGCPCoverage(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"GET " + gcpCoveragePath: {200, `{
	  "hubs": [{"hub_account_id": "gcp-hub", "coverage": "selected"}],
	  "assignments": [
	    {"account_id": "proj-a", "hub_account_id": "gcp-hub"},
	    {"account_id": "proj-x", "hub_account_id": "gcp-other"}
	  ]
	}`}})
	r := NewGCPScannerHubResource()
	s := configured(t, r, c)

	state, diags := doRead(t, r, s, gcpHubModel("selected", stringSet("proj-z")))
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodGet, URI: gcpCoveragePath})
	var got ScannerHubResourceModel
	getState(t, state, &got)
	if ids := setStrings(t, got.AccountIDs); len(ids) != 1 || ids[0] != "proj-a" {
		t.Errorf("account_ids = %v, want this hub's [proj-a]", ids)
	}
}

func TestGCPScannerHubConflictNamesProjects(t *testing.T) {
	_, c := newFakeAPI(t, gcpCreateRoutes(fakeResponse{409, `{"error":"Request failed with status code 409"}`}))
	r := NewGCPScannerHubResource()
	s := configured(t, r, c)

	_, diags := doCreate(t, r, s, gcpHubModel("all", types.SetNull(types.StringType)))

	requireError(t, diags, "409", "already scans all GCP projects", `only one hub may cover "all"`)
}

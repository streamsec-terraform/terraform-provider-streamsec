package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Azure: a hub is a subscription, which ms_accounts names "<tenant account id>:<subscription id>".

const (
	azureTenant       = "0123456789abcdef01234567"
	azureHubSub       = "00000000-0000-0000-0000-0000000000a1"
	azureSubB         = "00000000-0000-0000-0000-0000000000b2"
	azureSubC         = "00000000-0000-0000-0000-0000000000c3"
	azureHubPath      = "/api/accounts/accounts/" + azureTenant + ":" + azureHubSub + "/scanner-hub"
	azureCoveragePath = "/api/accounts/accounts/scanner-coverage?cloud=Azure"
)

func azureHubModel(coverage string, subscriptionIDs types.Set) AzureScannerHubResourceModel {
	return AzureScannerHubResourceModel{
		AccountID:       types.StringValue(azureTenant),
		SubscriptionID:  types.StringValue(azureHubSub),
		Coverage:        types.StringValue(coverage),
		SubscriptionIDs: subscriptionIDs,
	}
}

func azureCreateRoutes(put fakeResponse) map[string]fakeResponse {
	return map[string]fakeResponse{
		"POST /graphql":       accountsAnswer(`[{"_id":"` + azureTenant + `","account_type":"Azure"},{"_id":"aws-1","account_type":"AWS"}]`),
		"PUT " + azureHubPath: put,
	}
}

func TestAzureScannerHubCreatePutsSubscriptionKeys(t *testing.T) {
	api, c := newFakeAPI(t, azureCreateRoutes(fakeResponse{200, `{}`}))
	r := NewAzureScannerHubResource()
	s := configured(t, r, c)

	state, diags := doCreate(t, r, s, azureHubModel("selected", stringSet(azureSubC, azureSubB)))
	requireNoDiags(t, diags)

	api.expectCalls(accountsCall, apiCall{Method: http.MethodPut, URI: azureHubPath, Body: map[string]interface{}{
		"coverage": "selected", "accounts": []interface{}{azureTenant + ":" + azureSubB, azureTenant + ":" + azureSubC},
	}})
	var got AzureScannerHubResourceModel
	getState(t, state, &got)
	if ids := setStrings(t, got.SubscriptionIDs); len(ids) != 2 || ids[0] != azureSubB || ids[1] != azureSubC {
		t.Errorf("subscription_ids = %v, want the bare subscription ids", ids)
	}
}

func TestAzureScannerHubCreateRefusesAnAccountThatIsNotAzure(t *testing.T) {
	api, c := newFakeAPI(t, azureCreateRoutes(fakeResponse{200, `{}`}))
	r := NewAzureScannerHubResource()
	s := configured(t, r, c)

	model := azureHubModel("all", types.SetNull(types.StringType))
	model.AccountID = types.StringValue("aws-1")
	_, diags := doCreate(t, r, s, model)

	requireError(t, diags, "aws-1", "AWS", "streamsec_azure_scanner_hub takes an Azure tenant")
	api.expectCalls(accountsCall)
}

func TestAzureScannerHubReadKeepsItsTenantsSubscriptions(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"GET " + azureCoveragePath: {200, `{
	  "hubs": [{"hub_account_id": "` + azureTenant + `:` + azureHubSub + `", "coverage": "selected"}],
	  "assignments": [
	    {"account_id": "` + azureTenant + `:` + azureSubB + `", "hub_account_id": "` + azureTenant + `:` + azureHubSub + `"},
	    {"account_id": "fedcba9876543210fedcba98:` + azureSubC + `", "hub_account_id": "` + azureTenant + `:` + azureHubSub + `"},
	    {"account_id": "` + azureTenant + `:` + azureSubC + `", "hub_account_id": "` + azureTenant + `:other"}
	  ]
	}`}})
	r := NewAzureScannerHubResource()
	s := configured(t, r, c)

	state, diags := doRead(t, r, s, azureHubModel("all", types.SetNull(types.StringType)))
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodGet, URI: azureCoveragePath})
	var got AzureScannerHubResourceModel
	getState(t, state, &got)
	if got.Coverage.ValueString() != "selected" {
		t.Errorf("coverage = %s, want selected", got.Coverage)
	}
	if ids := setStrings(t, got.SubscriptionIDs); len(ids) != 1 || ids[0] != azureSubB {
		t.Errorf("subscription_ids = %v, want this hub's own tenant's [%s]", ids, azureSubB)
	}
}

func TestAzureScannerHubReadOfAGoneHubRemovesIt(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"GET " + azureCoveragePath: {200, `{"hubs": [], "assignments": []}`}})
	r := NewAzureScannerHubResource()
	s := configured(t, r, c)

	state, diags := doRead(t, r, s, azureHubModel("selected", stringSet(azureSubB)))
	requireNoDiags(t, diags)

	if !state.Raw.IsNull() {
		t.Errorf("a hub Stream Security no longer has stayed in state: %v", state.Raw)
	}
}

func TestAzureScannerHubDeleteTurnsTheSubscriptionsCoverageOff(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + azureHubPath: {200, `{}`}})
	r := NewAzureScannerHubResource()
	s := configured(t, r, c)

	requireNoDiags(t, doDelete(t, r, s, azureHubModel("selected", stringSet(azureSubB))))

	api.expectCalls(apiCall{Method: http.MethodPut, URI: azureHubPath, Body: map[string]interface{}{"coverage": "none"}})
}

func TestAzureScannerHubConflictNamesTheTenant(t *testing.T) {
	_, c := newFakeAPI(t, azureCreateRoutes(fakeResponse{409, `{"error":"Request failed with status code 409"}`}))
	r := NewAzureScannerHubResource()
	s := configured(t, r, c)

	_, diags := doCreate(t, r, s, azureHubModel("all", types.SetNull(types.StringType)))

	requireError(t, diags, "409", "all subscriptions of the tenant", `only one hub per tenant may cover "all"`,
		`set that hub to "selected" without the subscription`)
}

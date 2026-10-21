package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

const accessAckPath = "/api/accounts/accounts/hub-1/scanner-access-terraform-acknowledge"

func accessAckModel() AWSScannerAccessAckResourceModel {
	return AWSScannerAccessAckResourceModel{
		HubAccountID:        types.StringValue("hub-1"),
		SpokeCloudAccountID: types.StringValue("444455556666"),
	}
}

func TestScannerAccessAckCreateAcknowledgesDeployed(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + accessAckPath: {200, `{"status":"deployed","spoke_account_id":"444455556666"}`}})
	r := NewAWSScannerAccessAckResource()
	s := configured(t, r, c)

	state, diags := doCreate(t, r, s, accessAckModel())
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPut, URI: accessAckPath, Body: map[string]interface{}{
		"spoke_cloud_account_id": "444455556666", "status": "deployed",
	}})
	var got AWSScannerAccessAckResourceModel
	getState(t, state, &got)
	if got != accessAckModel() {
		t.Errorf("state = %+v", got)
	}
}

func TestScannerAccessAckDeleteAcknowledgesUninstalled(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + accessAckPath: {200, `{}`}})
	r := NewAWSScannerAccessAckResource()
	s := configured(t, r, c)

	requireNoDiags(t, doDelete(t, r, s, accessAckModel()))

	api.expectCalls(apiCall{Method: http.MethodPut, URI: accessAckPath, Body: map[string]interface{}{
		"spoke_cloud_account_id": "444455556666", "status": "uninstalled",
	}})
}

func TestScannerAccessAckReadKeepsStateWithoutCalls(t *testing.T) {
	api, c := newFakeAPI(t, nil)
	r := NewAWSScannerAccessAckResource()
	s := configured(t, r, c)

	state, diags := doRead(t, r, s, accessAckModel())
	requireNoDiags(t, diags)

	api.expectCalls()
	var got AWSScannerAccessAckResourceModel
	getState(t, state, &got)
	if got != accessAckModel() {
		t.Errorf("state = %+v", got)
	}
}

func TestScannerAccessAckCreateConflictIsExplained(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + accessAckPath: {409, `{"error":"Request failed with status code 409"}`}})
	r := NewAWSScannerAccessAckResource()
	s := configured(t, r, c)

	state, diags := doCreate(t, r, s, accessAckModel())

	requireError(t, diags, "409", "hub-1", "streamsec_aws_scanner_hub")
	if !state.Raw.IsNull() {
		t.Errorf("a failed create left state %v", state.Raw)
	}
}

func TestScannerAccessAckDeleteOfAGoneHubWarns(t *testing.T) {
	for _, status := range []int{404, 409} {
		_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + accessAckPath: {status, `{"error":"gone"}`}})
		r := NewAWSScannerAccessAckResource()
		s := configured(t, r, c)

		diags := doDelete(t, r, s, accessAckModel())

		if diags.HasError() || diags.WarningsCount() != 1 {
			t.Errorf("status %d: diagnostics = %s, want one warning", status, diagsText(diags))
		}
	}
}

func TestScannerAccessAckDeleteFailureIsAnError(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + accessAckPath: {403, `{"error":"Request failed with status code 403"}`}})
	r := NewAWSScannerAccessAckResource()
	s := configured(t, r, c)

	requireError(t, doDelete(t, r, s, accessAckModel()), "403")
}

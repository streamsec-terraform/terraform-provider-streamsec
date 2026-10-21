package provider

import (
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

const volumeAckPath = "/api/accounts/accounts/acc-1/scanner-terraform-acknowledge"

func volumeAckModel(moduleVersion string) AWSVolumeScannerAckResourceModel {
	return AWSVolumeScannerAckResourceModel{
		AccountID:     types.StringValue("acc-1"),
		Region:        types.StringValue("us-east-1"),
		ModuleVersion: types.StringValue(moduleVersion),
	}
}

func TestVolumeScannerAckCreateAcknowledgesDeployed(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + volumeAckPath: {200, `{"region":"us-east-1","status":"deployed"}`}})
	r := NewAWSVolumeScannerAckResource()
	s := configured(t, r, c)

	state, diags := doCreate(t, r, s, volumeAckModel("1.16.0"))
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPut, URI: volumeAckPath, Body: map[string]interface{}{
		"region": "us-east-1", "status": "deployed", "module_version": "1.16.0",
	}})
	var got AWSVolumeScannerAckResourceModel
	getState(t, state, &got)
	if got != volumeAckModel("1.16.0") {
		t.Errorf("state = %+v", got)
	}
}

func TestVolumeScannerAckUpdateAcknowledgesTheNewVersion(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + volumeAckPath: {200, `{}`}})
	r := NewAWSVolumeScannerAckResource()
	s := configured(t, r, c)

	state, diags := doUpdate(t, r, s, volumeAckModel("1.16.0"), volumeAckModel("1.17.0"))
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPut, URI: volumeAckPath, Body: map[string]interface{}{
		"region": "us-east-1", "status": "deployed", "module_version": "1.17.0",
	}})
	var got AWSVolumeScannerAckResourceModel
	getState(t, state, &got)
	if got.ModuleVersion.ValueString() != "1.17.0" {
		t.Errorf("state = %+v", got)
	}
}

func TestVolumeScannerAckDeleteAcknowledgesUninstalled(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + volumeAckPath: {200, `{}`}})
	r := NewAWSVolumeScannerAckResource()
	s := configured(t, r, c)

	requireNoDiags(t, doDelete(t, r, s, volumeAckModel("1.16.0")))

	api.expectCalls(apiCall{Method: http.MethodPut, URI: volumeAckPath, Body: map[string]interface{}{
		"region": "us-east-1", "status": "uninstalled", "module_version": "1.16.0",
	}})
}

func TestVolumeScannerAckReadKeepsStateWithoutCalls(t *testing.T) {
	api, c := newFakeAPI(t, nil)
	r := NewAWSVolumeScannerAckResource()
	s := configured(t, r, c)

	state, diags := doRead(t, r, s, volumeAckModel("1.16.0"))
	requireNoDiags(t, diags)

	api.expectCalls()
	var got AWSVolumeScannerAckResourceModel
	getState(t, state, &got)
	if got != volumeAckModel("1.16.0") {
		t.Errorf("state = %+v", got)
	}
}

func TestVolumeScannerAckCreateErrors(t *testing.T) {
	cases := []struct {
		name string
		resp fakeResponse
		want []string
	}{
		{"conflict explained", fakeResponse{409, `{"error":"Request failed with status code 409"}`}, []string{"409", "acc-1", "not a connected AWS account"}},
		{"not found explained", fakeResponse{404, `{"error":"Request failed with status code 404"}`}, []string{"404", "acc-1", "streamsec_aws_account"}},
		{"detail shown", fakeResponse{400, `{"detail":"invalid module_version"}`}, []string{"400", "invalid module_version"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + volumeAckPath: tc.resp})
			r := NewAWSVolumeScannerAckResource()
			s := configured(t, r, c)

			state, diags := doCreate(t, r, s, volumeAckModel("1.16.0"))

			requireError(t, diags, tc.want...)
			if !state.Raw.IsNull() {
				t.Errorf("a failed create left state %v", state.Raw)
			}
		})
	}
}

// An account already gone from Stream has no scanner to mark uninstalled; that
// must not block destroying the module.
func TestVolumeScannerAckDeleteOfAGoneAccountWarns(t *testing.T) {
	for _, status := range []int{404, 409} {
		_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + volumeAckPath: {status, `{"error":"gone"}`}})
		r := NewAWSVolumeScannerAckResource()
		s := configured(t, r, c)

		diags := doDelete(t, r, s, volumeAckModel("1.16.0"))

		if diags.HasError() || diags.WarningsCount() != 1 {
			t.Errorf("status %d: diagnostics = %s, want one warning", status, diagsText(diags))
		}
	}
}

func TestVolumeScannerAckDeleteFailureIsAnError(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + volumeAckPath: {500, `{"error":"boom"}`}})
	r := NewAWSVolumeScannerAckResource()
	s := configured(t, r, c)

	requireError(t, doDelete(t, r, s, volumeAckModel("1.16.0")), "500", "boom")
}

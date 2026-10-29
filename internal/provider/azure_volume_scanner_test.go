package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"terraform-provider-streamsec/internal/client"
)

const (
	azureSubscription      = "00000000-1111-2222-3333-444444444444"
	azureAckPath           = "/api/accounts/accounts/tenant-1/azure-scanner-terraform-acknowledge"
	azureScannerParamsPath = "/api/accounts/accounts/tenant-1/azure-scanner-terraform-params?subscription_id=" + azureSubscription
)

func azureAckModel(moduleVersion string, resourceGroup types.String) AzureVolumeScannerAckResourceModel {
	return AzureVolumeScannerAckResourceModel{
		TenantID:       types.StringValue("tenant-1"),
		SubscriptionID: types.StringValue(azureSubscription),
		ModuleVersion:  types.StringValue(moduleVersion),
		ResourceGroup:  resourceGroup,
	}
}

func TestAzureVolumeScannerAckCreateAcknowledgesDeployed(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + azureAckPath: {200, `{"status":"deployed"}`}})
	r := NewAzureVolumeScannerAckResource()
	s := configured(t, r, c)

	model := azureAckModel("1.6.0", types.StringValue("streamsec-scanner-tf-00000000"))
	state, diags := doCreate(t, r, s, model)
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPut, URI: azureAckPath, Body: map[string]interface{}{
		"subscription_id": azureSubscription, "status": "deployed", "module_version": "1.6.0",
		"resource_group": "streamsec-scanner-tf-00000000",
	}})
	var got AzureVolumeScannerAckResourceModel
	getState(t, state, &got)
	if got != model {
		t.Errorf("state = %+v", got)
	}
}

// Without a resource group the body leaves it out, so Stream Security keeps what it has.
func TestAzureVolumeScannerAckWithoutAResourceGroupSendsNone(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + azureAckPath: {200, `{}`}})
	r := NewAzureVolumeScannerAckResource()
	s := configured(t, r, c)

	_, diags := doCreate(t, r, s, azureAckModel("1.6.0", types.StringNull()))
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPut, URI: azureAckPath, Body: map[string]interface{}{
		"subscription_id": azureSubscription, "status": "deployed", "module_version": "1.6.0",
	}})
}

func TestAzureVolumeScannerAckUpdateAcknowledgesTheNewVersion(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + azureAckPath: {200, `{}`}})
	r := NewAzureVolumeScannerAckResource()
	s := configured(t, r, c)

	state, diags := doUpdate(t, r, s, azureAckModel("1.6.0", types.StringNull()), azureAckModel("1.7.0", types.StringNull()))
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPut, URI: azureAckPath, Body: map[string]interface{}{
		"subscription_id": azureSubscription, "status": "deployed", "module_version": "1.7.0",
	}})
	var got AzureVolumeScannerAckResourceModel
	getState(t, state, &got)
	if got.ModuleVersion.ValueString() != "1.7.0" {
		t.Errorf("state = %+v", got)
	}
}

func TestAzureVolumeScannerAckDeleteAcknowledgesUninstalled(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + azureAckPath: {200, `{}`}})
	r := NewAzureVolumeScannerAckResource()
	s := configured(t, r, c)

	requireNoDiags(t, doDelete(t, r, s, azureAckModel("1.6.0", types.StringNull())))

	api.expectCalls(apiCall{Method: http.MethodPut, URI: azureAckPath, Body: map[string]interface{}{
		"subscription_id": azureSubscription, "status": "uninstalled", "module_version": "1.6.0",
	}})
}

func TestAzureVolumeScannerAckReadKeepsStateWithoutCalls(t *testing.T) {
	api, c := newFakeAPI(t, nil)
	r := NewAzureVolumeScannerAckResource()
	s := configured(t, r, c)

	model := azureAckModel("1.6.0", types.StringNull())
	state, diags := doRead(t, r, s, model)
	requireNoDiags(t, diags)

	api.expectCalls()
	var got AzureVolumeScannerAckResourceModel
	getState(t, state, &got)
	if got != model {
		t.Errorf("state = %+v", got)
	}
}

func TestAzureVolumeScannerAckCreateErrors(t *testing.T) {
	cases := []struct {
		name string
		resp fakeResponse
		want []string
	}{
		{"conflict explained", fakeResponse{409, `{"error":"Request failed with status code 409"}`}, []string{"409", "tenant-1", "streamsec_azure_tenant_ack"}},
		{"not found explained", fakeResponse{404, `{"error":"Request failed with status code 404"}`}, []string{"404", "tenant-1", azureSubscription, "streamsec_azure_tenant"}},
		{"bad request names the fields", fakeResponse{400, `{"error":"Request failed with status code 400"}`}, []string{"400", "subscription_id", "module_version"}},
		{"forbidden explained", fakeResponse{403, `{"error":"Request failed with status code 403"}`}, []string{"403", "tenant-1", "trial"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + azureAckPath: tc.resp})
			r := NewAzureVolumeScannerAckResource()
			s := configured(t, r, c)

			state, diags := doCreate(t, r, s, azureAckModel("1.6.0", types.StringNull()))

			requireError(t, diags, tc.want...)
			if !state.Raw.IsNull() {
				t.Errorf("a failed create left state %v", state.Raw)
			}
		})
	}
}

// A tenant or subscription already gone from Stream must not block destroying the module.
func TestAzureVolumeScannerAckDeleteOfAGoneTenantWarns(t *testing.T) {
	for _, status := range []int{403, 404, 409} {
		_, c := newFakeAPI(t, map[string]fakeResponse{"PUT " + azureAckPath: {status, `{"error":"gone"}`}})
		r := NewAzureVolumeScannerAckResource()
		s := configured(t, r, c)

		diags := doDelete(t, r, s, azureAckModel("1.6.0", types.StringNull()))

		if diags.HasError() || diags.WarningsCount() != 1 {
			t.Errorf("status %d: diagnostics = %s, want one warning", status, diagsText(diags))
		}
	}
}

func readAzureVolumeScanner(t *testing.T, c *client.Client) (AzureVolumeScannerDataSourceModel, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	d := NewAzureVolumeScannerDataSource()
	configureResp := &datasource.ConfigureResponse{}
	d.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: c}, configureResp)
	requireNoDiags(t, configureResp.Diagnostics)
	schemaResp := &datasource.SchemaResponse{}
	d.Schema(ctx, datasource.SchemaRequest{}, schemaResp)
	requireNoDiags(t, schemaResp.Diagnostics)
	s := schemaResp.Schema

	config := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	requireNoDiags(t, config.Set(ctx, AzureVolumeScannerDataSourceModel{
		TenantID:        types.StringValue("tenant-1"),
		SubscriptionID:  types.StringValue(azureSubscription),
		CollectionToken: types.StringNull(),
	}))
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: config.Raw}}, resp)

	var got AzureVolumeScannerDataSourceModel
	if !resp.Diagnostics.HasError() {
		requireNoDiags(t, resp.State.Get(ctx, &got))
	}
	return got, resp.Diagnostics
}

func TestAzureVolumeScannerReadsTheSubscriptionsCollectionToken(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"GET " + azureScannerParamsPath: {200,
		`{"subscription_id":"` + azureSubscription + `","collection_token":"collect-secret"}`}})

	got, diags := readAzureVolumeScanner(t, c)
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodGet, URI: azureScannerParamsPath})
	if got.CollectionToken.ValueString() != "collect-secret" || got.SubscriptionID.ValueString() != azureSubscription {
		t.Errorf("model = %+v", got)
	}
}

func TestAzureVolumeScannerReadErrorsAreExplained(t *testing.T) {
	_, c := newFakeAPI(t, map[string]fakeResponse{"GET " + azureScannerParamsPath: {404, `{"error":"Request failed with status code 404"}`}})

	_, diags := readAzureVolumeScanner(t, c)

	requireError(t, diags, "404", "tenant-1", azureSubscription, "streamsec_azure_tenant")
}

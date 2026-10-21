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

const accessParamsPath = "/api/accounts/accounts/hub-1/scanner-access-params"

func readHubAccess(t *testing.T, c *client.Client) (AWSScannerHubAccessDataSourceModel, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	d := NewAWSScannerHubAccessDataSource()
	configureResp := &datasource.ConfigureResponse{}
	d.(datasource.DataSourceWithConfigure).Configure(ctx, datasource.ConfigureRequest{ProviderData: c}, configureResp)
	requireNoDiags(t, configureResp.Diagnostics)
	schemaResp := &datasource.SchemaResponse{}
	d.Schema(ctx, datasource.SchemaRequest{}, schemaResp)
	requireNoDiags(t, schemaResp.Diagnostics)
	s := schemaResp.Schema

	config := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	requireNoDiags(t, config.Set(ctx, AWSScannerHubAccessDataSourceModel{
		HubAccountID:            types.StringValue("hub-1"),
		HubCloudAccountID:       types.StringNull(),
		RoleName:                types.StringNull(),
		ExternalID:              types.StringNull(),
		TrustedRoleNamePatterns: types.ListNull(types.StringType),
	}))
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: config.Raw}}, resp)

	var got AWSScannerHubAccessDataSourceModel
	if !resp.Diagnostics.HasError() {
		requireNoDiags(t, resp.State.Get(ctx, &got))
	}
	return got, resp.Diagnostics
}

func TestScannerHubAccessReadsTheHubsAccessParams(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{"GET " + accessParamsPath: {200, `{
		"hub_cloud_account_id": "111122223333",
		"role_name": "StreamScannerSpokeRole",
		"external_id": "ext-secret",
		"trusted_role_name_patterns": ["streamsec-scanner-*", "*streamsec-ebs-scanner-tf-*"]
	}`}})

	got, diags := readHubAccess(t, c)
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodGet, URI: accessParamsPath})
	if got.HubAccountID.ValueString() != "hub-1" || got.HubCloudAccountID.ValueString() != "111122223333" ||
		got.RoleName.ValueString() != "StreamScannerSpokeRole" || got.ExternalID.ValueString() != "ext-secret" {
		t.Errorf("state = %+v", got)
	}
	var patterns []string
	requireNoDiags(t, got.TrustedRoleNamePatterns.ElementsAs(context.Background(), &patterns, false))
	if len(patterns) != 2 || patterns[0] != "streamsec-scanner-*" || patterns[1] != "*streamsec-ebs-scanner-tf-*" {
		t.Errorf("trusted_role_name_patterns = %v", patterns)
	}
}

func TestScannerHubAccessErrorsAreExplained(t *testing.T) {
	cases := []struct {
		name string
		resp fakeResponse
		want []string
	}{
		{"not a hub", fakeResponse{409, `{"error":"Request failed with status code 409"}`}, []string{"409", "hub-1", "streamsec_aws_scanner_hub", "depends_on"}},
		{"no such account", fakeResponse{404, `{"error":"Request failed with status code 404"}`}, []string{"404", "hub-1", "streamsec_aws_account"}},
		{"no access", fakeResponse{403, `{"detail":"No access to account hub-1"}`}, []string{"403", "No access to account hub-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, c := newFakeAPI(t, map[string]fakeResponse{"GET " + accessParamsPath: tc.resp})

			_, diags := readHubAccess(t, c)

			requireError(t, diags, tc.want...)
		})
	}
}

package provider

import (
	"context"
	"fmt"
	"net/http"

	"terraform-provider-streamsec/internal/client"
	"terraform-provider-streamsec/internal/utils"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &AWSScannerHubAccessDataSource{}
var _ datasource.DataSourceWithConfigure = &AWSScannerHubAccessDataSource{}

func NewAWSScannerHubAccessDataSource() datasource.DataSource {
	return &AWSScannerHubAccessDataSource{}
}

// AWSScannerHubAccessDataSource reads what a spoke's StreamScannerSpokeRole must
// trust for a hub's scanner to assume it.
type AWSScannerHubAccessDataSource struct {
	client *client.Client
}

type AWSScannerHubAccessDataSourceModel struct {
	HubAccountID            types.String `tfsdk:"hub_account_id"`
	HubCloudAccountID       types.String `tfsdk:"hub_cloud_account_id"`
	RoleName                types.String `tfsdk:"role_name"`
	ExternalID              types.String `tfsdk:"external_id"`
	TrustedRoleNamePatterns types.List   `tfsdk:"trusted_role_name_patterns"`
}

type scannerAccessParams struct {
	HubCloudAccountID       string   `json:"hub_cloud_account_id"`
	RoleName                string   `json:"role_name"`
	ExternalID              string   `json:"external_id"`
	TrustedRoleNamePatterns []string `json:"trusted_role_name_patterns"`
}

func scannerHubAccessHints(hubAccountID string) statusHints {
	return statusHints{
		http.StatusForbidden: fmt.Sprintf("The token has no access to account %s.", hubAccountID),
		http.StatusNotFound: fmt.Sprintf("Account %s is not in Stream Security: hub_account_id takes the hub's Stream Security account id "+
			"(streamsec_aws_account.<name>.id), not its 12-digit AWS account id.", hubAccountID),
		http.StatusConflict: fmt.Sprintf("Account %s's scanner does not scan other accounts yet, so it has no external id: make it a hub "+
			"with streamsec_aws_scanner_hub first. In the same configuration, add depends_on = [streamsec_aws_scanner_hub.<name>] "+
			"so this is read after the hub exists.", hubAccountID),
	}
}

func (d *AWSScannerHubAccessDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_scanner_hub_access"
}

func (d *AWSScannerHubAccessDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "What an AWS account's `StreamScannerSpokeRole` must trust so that a hub's scanner (`streamsec_aws_scanner_hub`) " +
			"can assume it: the hub's AWS account, its external id and its scanner role names.",

		Attributes: map[string]schema.Attribute{
			"hub_account_id": schema.StringAttribute{
				MarkdownDescription: "The Stream Security id of the hub's AWS account (`streamsec_aws_scanner_hub.<name>.account_id`).",
				Required:            true,
			},
			"hub_cloud_account_id": schema.StringAttribute{
				MarkdownDescription: "The hub's 12-digit AWS account id: the role trusts this account.",
				Computed:            true,
			},
			"role_name": schema.StringAttribute{
				MarkdownDescription: "The name the role must have: the hub's scanner assumes it by name in every account it scans.",
				Computed:            true,
			},
			"external_id": schema.StringAttribute{
				MarkdownDescription: "The hub's external id, for the role trust's `sts:ExternalId` condition.",
				Computed:            true,
				Sensitive:           true,
			},
			"trusted_role_name_patterns": schema.ListAttribute{
				MarkdownDescription: "Name patterns of the hub's scanner roles, for the role trust's `aws:PrincipalArn` condition " +
					"(`arn:<partition>:iam::<hub_cloud_account_id>:role/<pattern>`).",
				ElementType: types.StringType,
				Computed:    true,
			},
		},
	}
}

func (d *AWSScannerHubAccessDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

func (d *AWSScannerHubAccessDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data AWSScannerHubAccessDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hubID := data.HubAccountID.ValueString()
	var params scannerAccessParams
	err := d.client.DoRESTRequest(ctx, http.MethodGet, client.AccountsPath(hubID, "scanner-access-params"), nil, &params)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Unable to read the scanner hub's access parameters", err, scannerHubAccessHints(hubID))
		return
	}

	data.HubCloudAccountID = types.StringValue(params.HubCloudAccountID)
	data.RoleName = types.StringValue(params.RoleName)
	data.ExternalID = types.StringValue(params.ExternalID)
	data.TrustedRoleNamePatterns = utils.ConvertStringsArrayToTypesList(params.TrustedRoleNamePatterns)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

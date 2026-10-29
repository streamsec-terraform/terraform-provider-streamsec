package provider

import (
	"context"
	"net/http"
	"net/url"

	"terraform-provider-streamsec/internal/client"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &AzureVolumeScannerDataSource{}
var _ datasource.DataSourceWithConfigure = &AzureVolumeScannerDataSource{}

func NewAzureVolumeScannerDataSource() datasource.DataSource {
	return &AzureVolumeScannerDataSource{}
}

// AzureVolumeScannerDataSource reads what a Terraform-installed scanner in an Azure
// subscription carries: the subscription's collection token.
type AzureVolumeScannerDataSource struct {
	client *client.Client
}

type AzureVolumeScannerDataSourceModel struct {
	TenantID        types.String `tfsdk:"tenant_id"`
	SubscriptionID  types.String `tfsdk:"subscription_id"`
	CollectionToken types.String `tfsdk:"collection_token"`
}

type azureVolumeScannerParams struct {
	CollectionToken string `json:"collection_token"`
}

func (d *AzureVolumeScannerDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_azure_volume_scanner"
}

func (d *AzureVolumeScannerDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "What the agentless volume scanner in an Azure subscription needs from Stream Security: the subscription's " +
			"collection token, which the scanner reports and uploads with. The same token the console's ARM template carries.",

		Attributes: map[string]schema.Attribute{
			"tenant_id": schema.StringAttribute{
				MarkdownDescription: "The Stream Security id of the Azure tenant (`streamsec_azure_tenant.<name>.id`).",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"subscription_id": schema.StringAttribute{
				MarkdownDescription: "The Azure subscription the scanner runs in, one of the tenant's subscriptions in Stream Security.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(azureSubscriptionPattern, "must be an Azure subscription id"),
				},
			},
			"collection_token": schema.StringAttribute{
				MarkdownDescription: "The subscription's collection token.",
				Computed:            true,
				Sensitive:           true,
			},
		},
	}
}

func (d *AzureVolumeScannerDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

func (d *AzureVolumeScannerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data AzureVolumeScannerDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tenantID, subscriptionID := data.TenantID.ValueString(), data.SubscriptionID.ValueString()
	path := client.AccountsPath(tenantID, "azure-scanner-terraform-params") + "?subscription_id=" + url.QueryEscape(subscriptionID)
	var params azureVolumeScannerParams
	if err := d.client.DoRESTRequest(ctx, http.MethodGet, path, nil, &params); err != nil {
		addAPIError(&resp.Diagnostics, "Unable to read the Azure volume scanner's parameters", err,
			azureVolumeScannerHints(tenantID, subscriptionID))
		return
	}

	data.CollectionToken = types.StringValue(params.CollectionToken)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

package provider

import (
	"context"
	"fmt"
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
	AccountID       types.String `tfsdk:"account_id"`
	SubscriptionID  types.String `tfsdk:"subscription_id"`
	CollectionToken types.String `tfsdk:"collection_token"`
}

type azureVolumeScannerParams struct {
	CollectionToken string `json:"collection_token"`
}

func azureVolumeScannerHints(accountID, subscriptionID string) statusHints {
	return statusHints{
		http.StatusBadRequest: "Stream Security refused subscription_id: it must be an Azure subscription id.",
		http.StatusForbidden: fmt.Sprintf("The token has no access to Azure tenant %s or subscription %s, or is read-only: "+
			"the collection token is a credential that writes scan data.", accountID, subscriptionID),
		http.StatusNotFound: fmt.Sprintf("Either tenant %s is not in Stream Security, or subscription %s is not one of its subscriptions: "+
			"account_id takes the Stream Security id of the tenant (streamsec_azure_tenant.<name>.id).", accountID, subscriptionID),
		http.StatusConflict: fmt.Sprintf("Azure tenant %s is not connected to Stream Security yet, or is being deleted. "+
			"In the configuration that connects it, add depends_on = [streamsec_azure_tenant_ack.<name>] so this is read after it is.",
			accountID),
	}
}

func (d *AzureVolumeScannerDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_azure_volume_scanner"
}

func (d *AzureVolumeScannerDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "What the agentless volume scanner in an Azure subscription needs from Stream Security: the collection " +
			"token Stream Security keeps for the subscription, which the scanner reports and uploads with. It is made on the first " +
			"read and stays the same, for a scanner installed from the console too. Needs a token that is not read-only.",

		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				MarkdownDescription: "The Stream Security id of the Azure tenant (`streamsec_azure_tenant.<name>.id`), not the Azure tenant id.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(streamsecAccountIDPattern,
						"must be the Stream Security id of the tenant (streamsec_azure_tenant.<name>.id), not the Azure tenant id"),
				},
			},
			"subscription_id": schema.StringAttribute{
				MarkdownDescription: "The Azure subscription the scanner runs in, one of the tenant's subscriptions in Stream Security, in lowercase.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(azureSubscriptionPattern, "must be an Azure subscription id, in lowercase"),
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

	accountID, subscriptionID := data.AccountID.ValueString(), data.SubscriptionID.ValueString()
	path := client.AccountsPath(accountID, "azure-scanner-terraform-params") + "?subscription_id=" + url.QueryEscape(subscriptionID)
	var params azureVolumeScannerParams
	if err := d.client.DoRESTRequest(ctx, http.MethodGet, path, nil, &params); err != nil {
		addAPIError(&resp.Diagnostics, "Unable to read the Azure volume scanner's parameters", err,
			azureVolumeScannerHints(accountID, subscriptionID))
		return
	}
	if params.CollectionToken == "" {
		resp.Diagnostics.AddError("Unable to read the Azure volume scanner's parameters",
			fmt.Sprintf("Stream Security returned no collection token for subscription %s.", subscriptionID))
		return
	}

	data.CollectionToken = types.StringValue(params.CollectionToken)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

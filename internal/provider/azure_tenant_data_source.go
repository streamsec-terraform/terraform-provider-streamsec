// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"terraform-provider-streamsec/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &AzureTenantDataSource{}

func NewAzureTenantDataSource() datasource.DataSource {
	return &AzureTenantDataSource{}
}

// AzureTenantDataSource defines the data source implementation.
type AzureTenantDataSource struct {
	client *client.Client
}

// AzureTenantDataSourceModel describes the data source data model.
type AzureTenantDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	CloudAccountID types.String `tfsdk:"tenant_id"`
	DisplayName    types.String `tfsdk:"display_name"`
	TemplateURL    types.String `tfsdk:"template_url"`
	AccountToken   types.String `tfsdk:"account_token"`
}

func (d *AzureTenantDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_azure_tenant"
}

func (d *AzureTenantDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "AzureTenant data source",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The internal ID of the account.",
				Computed:            true,
			},
			"tenant_id": schema.StringAttribute{
				MarkdownDescription: "azure tenant id",
				Required:            true,
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "The display name of the tenant.",
				Computed:            true,
			},
			"template_url": schema.StringAttribute{
				MarkdownDescription: "The template URL of the tenant.",
				Computed:            true,
			},
			"account_token": schema.StringAttribute{
				MarkdownDescription: "The account token of the tenant.",
				Computed:            true,
			},
		},
	}
}

func (d *AzureTenantDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*client.Client)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *http.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = client
}

func (d *AzureTenantDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data AzureTenantDataSourceModel

	// Read Terraform configuration data into the model
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	query := `
		query {
			accounts {
				_id
				account_type
				cloud_account_id
				display_name
				cloud_regions
				template_url
				external_id
				lightlytics_collection_token
				account_token
				status
			}
		}`

	res, err := d.client.DoRequest(query, nil)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to get account, got error: %s", err))
		return
	}

	accounts := res["accounts"].([]interface{})
	accountFound := false

	tflog.Debug(ctx, fmt.Sprintf("accounts: %v", accounts))

	for _, acc := range accounts {

		account := acc.(map[string]interface{})
		if account["cloud_account_id"] != nil && account["cloud_account_id"].(string) == data.CloudAccountID.ValueString() {
			if account["status"].(string) == "DELETING" {
				resp.Diagnostics.AddError("Resource status is DELETING", fmt.Sprintf("Azure tenant with id: %s is being deleted.", data.CloudAccountID.ValueString()))
				return
			}
			data.ID = types.StringValue(account["_id"].(string))
			data.DisplayName = types.StringValue(account["display_name"].(string))
			data.TemplateURL = types.StringValue(account["template_url"].(string))
			data.AccountToken = types.StringValue(account["account_token"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get Azure tenant, Azure tenant with id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	// Write logs using the tflog package
	// Documentation: https://terraform.io/plugin/log
	tflog.Trace(ctx, "read a data source")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

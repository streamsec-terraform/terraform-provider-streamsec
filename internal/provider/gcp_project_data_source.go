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
var _ datasource.DataSource = &GCPProjectDataSource{}

func NewGCPProjectDataSource() datasource.DataSource {
	return &GCPProjectDataSource{}
}

// GCPProjectDataSource defines the data source implementation.
type GCPProjectDataSource struct {
	client *client.Client
}

// GCPProjectDataSourceModel describes the data source data model.
type GCPProjectDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	CloudAccountID types.String `tfsdk:"project_id"`
	DisplayName    types.String `tfsdk:"display_name"`
	AccountToken   types.String `tfsdk:"account_token"`
}

func (d *GCPProjectDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gcp_project"
}

func (d *GCPProjectDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "GCPProject data source",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The internal ID of the account.",
				Computed:            true,
			},
			"project_id": schema.StringAttribute{
				MarkdownDescription: "The GCP Project ID.",
				Required:            true,
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "The display name of the project.",
				Computed:            true,
			},
			"account_token": schema.StringAttribute{
				MarkdownDescription: "The account token of the project.",
				Computed:            true,
			},
		},
	}
}

func (d *GCPProjectDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *GCPProjectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data GCPProjectDataSourceModel

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
		if account["cloud_account_id"].(string) == data.CloudAccountID.ValueString() {
			if account["status"].(string) == "DELETING" {
				resp.Diagnostics.AddError("Resource status is DELETING", fmt.Sprintf("GCP project with id: %s is being deleted.", data.CloudAccountID.ValueString()))
				return
			}
			data.ID = types.StringValue(account["_id"].(string))
			data.DisplayName = types.StringValue(account["display_name"].(string))
			data.AccountToken = types.StringValue(account["account_token"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	// Write logs using the tflog package
	// Documentation: https://terraform.io/plugin/log
	tflog.Trace(ctx, "read a data source")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

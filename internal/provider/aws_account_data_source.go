// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"terraform-provider-streamsec/internal/client"
	"terraform-provider-streamsec/internal/utils"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &AWSAccountDataSource{}

func NewAWSAccountDataSource() datasource.DataSource {
	return &AWSAccountDataSource{}
}

// AWSAccountDataSource defines the data source implementation.
type AWSAccountDataSource struct {
	client *client.Client
}

// AWSAccountDataSourceModel describes the data source data model.
type AWSAccountDataSourceModel struct {
	ID                       types.String `tfsdk:"id"`
	CloudAccountID           types.String `tfsdk:"cloud_account_id"`
	DisplayName              types.String `tfsdk:"display_name"`
	CloudRegions             types.List   `tfsdk:"cloud_regions"`
	TemplateURL              types.String `tfsdk:"template_url"`
	ExternalID               types.String `tfsdk:"external_id"`
	StreamSecCollectionToken types.String `tfsdk:"streamsec_collection_token"`
	AccountAuthToken         types.String `tfsdk:"account_auth_token"`
}

func (d *AWSAccountDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_account"
}

func (d *AWSAccountDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "AWSAccount data source",

		Attributes: map[string]schema.Attribute{
			"cloud_account_id": schema.StringAttribute{
				MarkdownDescription: "aws account id",
				Required:            true,
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "The internal ID of the account.",
				Computed:            true,
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "The display name of the account.",
				Computed:            true,
			},
			"cloud_regions": schema.ListAttribute{
				MarkdownDescription: "The cloud regions of the account.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"template_url": schema.StringAttribute{
				MarkdownDescription: "The template URL of the account.",
				Computed:            true,
			},
			"external_id": schema.StringAttribute{
				MarkdownDescription: "The external ID of the account.",
				Computed:            true,
			},
			"streamsec_collection_token": schema.StringAttribute{
				MarkdownDescription: "The Stream Security collection token of the account.",
				Computed:            true,
			},
			"account_auth_token": schema.StringAttribute{
				MarkdownDescription: "The account auth token of the account.",
				Computed:            true,
			},
		},
	}
}

func (d *AWSAccountDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *AWSAccountDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data AWSAccountDataSourceModel

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
				account_auth_token
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

	for _, acc := range accounts {

		account := acc.(map[string]interface{})
		if account["cloud_account_id"].(string) == data.CloudAccountID.ValueString() {
			if account["status"].(string) == "DELETING" {
				resp.Diagnostics.AddError("Resource status is DELETING", fmt.Sprintf("Account with cloud_account_id: %s is being deleted.", data.CloudAccountID.ValueString()))
				return
			}
			data.ID = types.StringValue(account["_id"].(string))
			data.DisplayName = types.StringValue(account["display_name"].(string))
			data.CloudRegions = utils.ConvertInterfaceToTypesList(account["cloud_regions"].([]interface{}))
			data.TemplateURL = types.StringValue(account["template_url"].(string))
			data.ExternalID = types.StringValue(account["external_id"].(string))
			data.StreamSecCollectionToken = types.StringValue(account["lightlytics_collection_token"].(string))
			data.AccountAuthToken = types.StringValue(account["account_auth_token"].(string))
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

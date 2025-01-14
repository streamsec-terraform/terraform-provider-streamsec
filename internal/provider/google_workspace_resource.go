package provider

import (
	"context"
	"fmt"
	"terraform-provider-streamsec/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GoogleWorkspaceResource{}
var _ resource.ResourceWithImportState = &GoogleWorkspaceResource{}

func NewGoogleWorkspaceResource() resource.Resource {
	return &GoogleWorkspaceResource{}
}

type GoogleWorkspaceResource struct {
	client *client.Client
}
type GoogleWorkspaceResourceModel struct {
	ID             types.String `tfsdk:"id"`
	DisplayName    types.String `tfsdk:"display_name"`
	CloudAccountID types.String `tfsdk:"customer_id"`
	ClientEmail    types.String `tfsdk:"client_email"`
	PrivateKey     types.String `tfsdk:"private_key"`
	AccountToken   types.String `tfsdk:"account_token"`
}

func (r *GoogleWorkspaceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_google_workspace"
}

func (r *GoogleWorkspaceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "GoogleWorkspace resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The ID of the account.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"display_name": schema.StringAttribute{
				Description: "The display name of the account.",
				Required:    true,
			},
			"customer_id": schema.StringAttribute{
				Description: "The Google Workspace Customer ID.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"client_email": schema.StringAttribute{
				Description: "The service account email.",
				Required:    true,
			},
			"private_key": schema.StringAttribute{
				Description: "The service account private key.",
				Required:    true,
				Sensitive:   true,
			},
			"account_token": schema.StringAttribute{
				Description: "The account token.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *GoogleWorkspaceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*client.Client)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *GoogleWorkspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GoogleWorkspaceResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	query := `
		mutation CreateAccount($account_type: CloudProvider!, $cloud_account_id: String!, $display_name: String) {
			createAccount(account: {
				account_type: $account_type,
				cloud_account_id: $cloud_account_id,
				display_name: $display_name,
			  })
			{
				_id
				account_token
			}
	}`

	variables := map[string]interface{}{
		"account_type":     "GOOGLE_WORKSPACE",
		"cloud_account_id": data.CloudAccountID.ValueString(),
		"display_name":     data.DisplayName.ValueString(),
		"client_email":     data.ClientEmail.ValueString(),
		"private_key":      data.PrivateKey.ValueString(),
	}

	tflog.Debug(ctx, fmt.Sprintf("variables: %v", variables))

	res, err := r.client.DoRequest(query, variables)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create account, got error: %s", err))
		return
	}

	tflog.Debug(ctx, fmt.Sprintf("Response: %v", res))

	account := res["createAccount"].(map[string]interface{})
	data.ID = types.StringValue(account["_id"].(string))
	data.AccountToken = types.StringValue(account["account_token"].(string))

	// Write logs using the tflog package
	// Documentation: https://terraform.io/plugin/log
	tflog.Trace(ctx, "created a resource")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GoogleWorkspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data GoogleWorkspaceResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

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

	res, err := r.client.DoRequest(query, nil)

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
				resp.Diagnostics.AddError("Resource status is DELETING", fmt.Sprintf("Google workspace with id: %s is being deleted.", data.CloudAccountID.ValueString()))
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
}

func (r *GoogleWorkspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GoogleWorkspaceResourceModel
	var state GoogleWorkspaceResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// check if there was a change in display_name
	if data.DisplayName != state.DisplayName || data.ClientEmail != state.ClientEmail || data.PrivateKey != state.PrivateKey {
		query := `
			mutation UpdateAccount($id: ID!, $account: AccountUpdateInput) {
				updateAccount(id: $id, account: $account) {
					_id
				}
			}`

		variables := map[string]interface{}{
			"id": data.ID.ValueString(),
			"account": map[string]interface{}{
				"display_name": data.DisplayName.ValueString(),
				"client_email": data.ClientEmail.ValueString(),
				"private_key":  data.PrivateKey.ValueString(),
			}}

		tflog.Debug(ctx, fmt.Sprintf("variables: %v", variables))

		_, err := r.client.DoRequest(query, variables)

		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update account, got error: %s", err))
			return
		}

	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GoogleWorkspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data GoogleWorkspaceResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	query := `
		mutation DeleteAccount($id: ID!) {
			deleteAccount(id: $id)
		}`

	variables := map[string]interface{}{
		"id": data.ID.ValueString()}

	_, err := r.client.DoRequest(query, variables)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete account, got error: %s", err))
		return
	}
}

func (r *GoogleWorkspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("customer_id"), req, resp)
}

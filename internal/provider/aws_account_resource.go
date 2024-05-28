package provider

import (
	"context"
	"fmt"
	"regexp"
	"terraform-provider-streamsec/internal/client"
	"terraform-provider-streamsec/internal/utils"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &AWSAccountResource{}
var _ resource.ResourceWithImportState = &AWSAccountResource{}

func NewAWSAccountResource() resource.Resource {
	return &AWSAccountResource{}
}

type AWSAccountResource struct {
	client *client.Client
}
type AWSAccountResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	DisplayName              types.String `tfsdk:"display_name"`
	CloudAccountID           types.String `tfsdk:"cloud_account_id"`
	CloudRegions             types.List   `tfsdk:"cloud_regions"`
	StackRegion              types.String `tfsdk:"stack_region"`
	TemplateURL              types.String `tfsdk:"template_url"`
	ExternalID               types.String `tfsdk:"external_id"`
	StreamSecCollectionToken types.String `tfsdk:"streamsec_collection_token"`
	AccountAuthToken         types.String `tfsdk:"account_auth_token"`
}

func (r *AWSAccountResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_account"
}

func (r *AWSAccountResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "AWSAccount resource",

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
			"cloud_account_id": schema.StringAttribute{
				Description: "The cloud account ID.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				// add validator to check if the cloud_account_id is a valid AWS account ID
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^(\d{12})$`), "The cloud account ID must be a 12-digit number."),
				},
			},
			"cloud_regions": schema.ListAttribute{
				ElementType: types.StringType,
				Description: "The cloud regions.",
				Required:    true,
			},
			"stack_region": schema.StringAttribute{
				Description: "The stack region.",
				Required:    true,
			},
			"template_url": schema.StringAttribute{
				Description: "The template URL.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"external_id": schema.StringAttribute{
				Description: "The external ID.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"streamsec_collection_token": schema.StringAttribute{
				Description: "The Streamsec collection token.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"account_auth_token": schema.StringAttribute{
				Description: "The account auth token.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *AWSAccountResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AWSAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AWSAccountResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	query := `
		mutation CreateAccount($account_type: CloudProvider!, $cloud_account_id: String!, $display_name: String, $cloud_regions: [String], $stack_region: String) {
			createAccount(account: {
				account_type: $account_type,
				cloud_account_id: $cloud_account_id,
				display_name: $display_name,
				cloud_regions: $cloud_regions,
				stack_region: $stack_region
			  })
			{
				_id
				template_url
				external_id
				lightlytics_collection_token
				account_auth_token
			}
	}`

	variables := map[string]interface{}{
		"account_type":     "AWS",
		"cloud_account_id": data.CloudAccountID.ValueString(),
		"display_name":     data.DisplayName.ValueString(),
		"cloud_regions":    utils.ConvertToStringSlice(data.CloudRegions.Elements()), // Fix: Access the Value field directly
		"stack_region":     data.StackRegion.ValueString(),
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
	data.TemplateURL = types.StringValue(account["template_url"].(string))
	data.ExternalID = types.StringValue(account["external_id"].(string))
	data.StreamSecCollectionToken = types.StringValue(account["lightlytics_collection_token"].(string))
	data.AccountAuthToken = types.StringValue(account["account_auth_token"].(string))

	// Write logs using the tflog package
	// Documentation: https://terraform.io/plugin/log
	tflog.Trace(ctx, "created a resource")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AWSAccountResourceModel

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
				cloud_regions
				stack_region
				template_url
				external_id
				lightlytics_collection_token
				account_auth_token
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
			data.ID = types.StringValue(account["_id"].(string))
			data.DisplayName = types.StringValue(account["display_name"].(string))
			data.CloudRegions = utils.ConvertInterfaceToTypesList(account["cloud_regions"].([]interface{}))
			data.StackRegion = types.StringValue(account["stack_region"].(string))
			data.TemplateURL = types.StringValue(account["template_url"].(string))
			data.ExternalID = types.StringValue(account["external_id"].(string))
			data.StreamSecCollectionToken = types.StringValue(account["lightlytics_collection_token"].(string))
			data.AccountAuthToken = types.StringValue(account["account_auth_token"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get account, account with cloud_account_id: %s not found in Stream.Security API and probably deleted manually."+
			"Please remove the resource from the terraform state.", data.CloudAccountID.ValueString()))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AWSAccountResourceModel
	var state AWSAccountResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// check if there was a change in display_name
	if data.DisplayName != state.DisplayName || !utils.EqualListValues(data.CloudRegions, state.CloudRegions) {
		query := `
			mutation UpdateAccount($id: ID!, $account: AccountUpdateInput) {
				updateAccount(id: $id, account: $account) {
					_id
				}
			}`

		variables := map[string]interface{}{
			"id": data.ID.ValueString(),
			"account": map[string]interface{}{
				"cloud_regions": utils.ConvertToStringSlice(data.CloudRegions.Elements()),
				"display_name":  data.DisplayName.ValueString()}}

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

func (r *AWSAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AWSAccountResourceModel

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

func (r *AWSAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

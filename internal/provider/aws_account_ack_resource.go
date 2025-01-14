package provider

import (
	"context"
	"fmt"
	"regexp"
	"terraform-provider-streamsec/internal/client"

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
var _ resource.Resource = &AWSAccountAckResource{}
var _ resource.ResourceWithImportState = &AWSAccountAckResource{}

func NewAWSAccountAckResource() resource.Resource {
	return &AWSAccountAckResource{}
}

type AWSAccountAckResource struct {
	client *client.Client
}
type AWSAccountAckResourceModel struct {
	ID             types.String `tfsdk:"id"`
	RoleARN        types.String `tfsdk:"role_arn"`
	CloudAccountID types.String `tfsdk:"cloud_account_id"`
	StackRegion    types.String `tfsdk:"stack_region"`
}

func (r *AWSAccountAckResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_account_ack"
}

func (r *AWSAccountAckResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "AWSAccountAck resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The internal ID of the account.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role_arn": schema.StringAttribute{
				Description: "The role that gives permissions to Stream.Security.",
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
			"stack_region": schema.StringAttribute{
				Description: "The stack region.",
				Required:    true,
			},
		},
	}
}

func (r *AWSAccountAckResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AWSAccountAckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AWSAccountAckResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

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
	account_auth_token := ""

	for _, acc := range accounts {

		account := acc.(map[string]interface{})
		if account["cloud_account_id"].(string) == data.CloudAccountID.ValueString() {
			data.ID = types.StringValue(account["_id"].(string))
			accountFound = true
			account_auth_token = account["account_auth_token"].(string)
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get account, account with cloud_account_id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Account found: %v", data))
	tflog.Info(ctx, fmt.Sprintf("Account auth token: %v", account_auth_token))

	query = `
		mutation AccountAcknowledge($input: AccountAckInput){
        accountAcknowledge(account: $input)
    }`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"lightlytics_internal_account_id": data.ID.ValueString(),
			"role_arn":                        data.RoleARN.ValueString(),
			"account_type":                    "AWS",
			"account_aliases":                 "",
			"cloud_account_id":                data.CloudAccountID.ValueString(),
			"stack_region":                    data.StackRegion.ValueString(),
			"stack_id":                        "",
			"init_stack_version":              1,
		},
	}

	tflog.Debug(ctx, fmt.Sprintf("variables: %v", variables))

	res, err = r.client.DoRequestWithToken(query, variables, account_auth_token)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to send account acknowledge, got error: %s", err))
		return
	}

	tflog.Debug(ctx, fmt.Sprintf("Response: %v", res))

	// Write logs using the tflog package
	// Documentation: https://terraform.io/plugin/log
	tflog.Trace(ctx, "created a resource")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSAccountAckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AWSAccountAckResourceModel

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
				role_arn
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
			data.CloudAccountID = types.StringValue(account["cloud_account_id"].(string))
			data.StackRegion = types.StringValue(account["stack_region"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSAccountAckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AWSAccountAckResourceModel
	var state AWSAccountAckResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

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
	account_auth_token := ""

	for _, acc := range accounts {

		account := acc.(map[string]interface{})
		if account["cloud_account_id"].(string) == data.CloudAccountID.ValueString() {
			data.ID = types.StringValue(account["_id"].(string))
			accountFound = true
			account_auth_token = account["account_auth_token"].(string)
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get account, account with cloud_account_id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Account found: %v", data))
	tflog.Info(ctx, fmt.Sprintf("Account auth token: %v", account_auth_token))

	// check if there was a change in display_name
	if data.RoleARN != state.RoleARN {
		query := `
			mutation accountUpdateAcknowledge($account: AccountUpdateAckInput) {
				accountUpdateAcknowledge(account: $account)
			}`

		variables := map[string]interface{}{
			"account": map[string]interface{}{
				"lightlytics_internal_account_id": data.ID.ValueString(),
				"account_type":                    "AWS",
				"role_arn":                        data.RoleARN.ValueString(),
				"init_stack_version":              1,
			},
		}

		tflog.Debug(ctx, fmt.Sprintf("variables: %v", variables))

		_, err := r.client.DoRequestWithToken(query, variables, account_auth_token)

		if err != nil {
			resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update account, got error: %s", err))
			return
		}

	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSAccountAckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AWSAccountAckResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *AWSAccountAckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("cloud_account_id"), req, resp)
}

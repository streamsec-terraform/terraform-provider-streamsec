package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"terraform-provider-streamsec/internal/client"
	"terraform-provider-streamsec/internal/utils"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
var _ resource.Resource = &AWSResponseAckResource{}
var _ resource.ResourceWithImportState = &AWSResponseAckResource{}

func NewAWSResponseAckResource() resource.Resource {
	return &AWSResponseAckResource{}
}

type RemediationRequestBody struct {
	AccountId       string            `json:"AccountId"`
	Region          string            `json:"Region"`
	TemplateVersion string            `json:"TemplateVersion"`
	ExternalId      string            `json:"ExternalId"`
	RoleARN         string            `json:"RoleARN"`
	StackId         string            `json:"StackId"`
	RunbookList     []string          `json:"RunbookList"`
	RunbookRoleList []string          `json:"RunbookRoleList"`
	PolicyToRoleMap map[string]string `json:"PolicyToRoleMap"`
}

type AWSResponseAckResource struct {
	client *client.Client
}
type AWSResponseAckResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	CloudAccountID           types.String `tfsdk:"cloud_account_id"`
	Region                   types.String `tfsdk:"region"`
	StreamsecCollectionToken types.String `tfsdk:"streamsec_collection_token"`
	RoleARN                  types.String `tfsdk:"role_arn"`
	RunbookList              types.List   `tfsdk:"runbook_list"`
	RunbookRoleList          types.List   `tfsdk:"runbook_role_list"`
	PolicyToRoleMap          types.Map    `tfsdk:"policy_to_role_map"`
	ExternalId               types.String `tfsdk:"external_id"`
}

func (r *AWSResponseAckResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_response_ack"
}

func (r *AWSResponseAckResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "AWSResponseAck resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The internal ID of the account.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"region": schema.StringAttribute{
				Description: "The region to ack.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
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
			"role_arn": schema.StringAttribute{
				Description: "The remediation role ARN.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"runbook_list": schema.ListAttribute{
				ElementType: types.StringType,
				Description: "The runbook list.",
				Required:    true,
			},
			"runbook_role_list": schema.ListAttribute{
				ElementType: types.StringType,
				Description: "The runbook role list.",
				Required:    true,
			},
			"policy_to_role_map": schema.MapAttribute{
				ElementType: types.StringType,
				Description: "The policy to role map.",
				Required:    true,
			},
			"external_id": schema.StringAttribute{
				Description: "The external ID.",
				Required:    true,
			},
			"streamsec_collection_token": schema.StringAttribute{
				Description: "The collection token.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *AWSResponseAckResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AWSResponseAckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AWSResponseAckResourceModel

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
				remediation {
					status
					role_arn
					stack_id
					runbook_list
					runbook_role_list
					policy_to_role_map
					external_id
				}
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
			if account["remediation"] != nil {
				if account["remediation"].(map[string]interface{})["status"].(string) == "READY" {
					resp.Diagnostics.AddError("Client Error", "Account remediation is already enabled")
					return
				}
			}
			data.ID = types.StringValue(account["_id"].(string))
			data.StreamsecCollectionToken = types.StringValue(account["lightlytics_collection_token"].(string))
			data.RoleARN = types.StringValue(account["remediation"].(map[string]interface{})["role_arn"].(string))
			data.RunbookList = types.ListValueMust(types.StringType, account["remediation"].(map[string]interface{})["runbook_list"].([]attr.Value))
			data.RunbookRoleList = types.ListValueMust(types.StringType, account["remediation"].(map[string]interface{})["runbook_role_list"].([]attr.Value))
			data.PolicyToRoleMap = types.MapValueMust(types.StringType, account["remediation"].(map[string]interface{})["policy_to_role_map"].(map[string]attr.Value))
			data.ExternalId = types.StringValue(account["remediation"].(map[string]interface{})["external_id"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get account, account with cloud_account_id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	body := RemediationRequestBody{
		AccountId:       data.CloudAccountID.ValueString(),
		Region:          data.Region.ValueString(),
		TemplateVersion: "1",
		ExternalId:      data.ExternalId.ValueString(),
		RoleARN:         data.RoleARN.ValueString(),
		StackId:         "terraform",
		RunbookList:     utils.ConvertToStringSlice(data.RunbookList.Elements()),
		RunbookRoleList: utils.ConvertToStringSlice(data.RunbookRoleList.Elements()),
		PolicyToRoleMap: utils.ConvertToStringMap(data.PolicyToRoleMap.Elements()),
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		fmt.Printf("Error marshalling JSON: %s\n", err)
		return
	}

	url := fmt.Sprintf("https://%s/api/accounts/accounts/remediation-acknowledge", r.client.Host)

	ackReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	ackReq.Header.Set("Authorization", "Bearer "+data.StreamsecCollectionToken.ValueString())

	tflog.Debug(ctx, fmt.Sprintf("Request: %v", ackReq))

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to ack region, got error: %s", err))
		return
	}

	ack, err := http.DefaultClient.Do(ackReq)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to ack region, got error: %s", err))
		return
	}

	if ack.StatusCode != 200 {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create account, got error: %s", ack.Status))
		return
	}

	tflog.Debug(ctx, fmt.Sprintf("Response: %v", res))

	// Write logs using the tflog package
	// Documentation: https://terraform.io/plugin/log
	tflog.Trace(ctx, "created a resource")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSResponseAckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AWSResponseAckResourceModel

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
				template_url
				external_id
				lightlytics_collection_token
				account_auth_token
				remediation {
					status
					role_arn
					stack_id
					runbook_list
					runbook_role_list
					policy_to_role_map
				}
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
			if account["remediation"] != nil {
				if account["remediation"].(map[string]interface{})["status"].(string) == "READY" {
					accountFound = true
				}
			}
			if !accountFound {
				resp.State.RemoveResource(ctx)
				return
			}
			data.ID = types.StringValue(account["_id"].(string))
			data.StreamsecCollectionToken = types.StringValue(account["lightlytics_collection_token"].(string))
			data.RoleARN = types.StringValue(account["remediation"].(map[string]interface{})["role_arn"].(string))
			data.RunbookList = types.ListValueMust(types.StringType, account["remediation"].(map[string]interface{})["runbook_list"].([]attr.Value))
			data.RunbookRoleList = types.ListValueMust(types.StringType, account["remediation"].(map[string]interface{})["runbook_role_list"].([]attr.Value))
			data.PolicyToRoleMap = types.MapValueMust(types.StringType, account["policy_to_role_map"].(map[string]attr.Value))
			data.ExternalId = types.StringValue(account["external_id"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSResponseAckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AWSResponseAckResourceModel
	var state AWSResponseAckResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSResponseAckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AWSResponseAckResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	url := fmt.Sprintf("https://%s/api/accounts/accounts/remediation/%s", r.client.Host, data.CloudAccountID.ValueString())

	ackReq, err := http.NewRequest("DELETE", url, nil)
	ackReq.Header.Set("Authorization", "Bearer "+data.StreamsecCollectionToken.ValueString())

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete remediation, got error: %s", err))
		return
	}

	ack, err := http.DefaultClient.Do(ackReq)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete remediation, got error: %s", err))
		return
	}

	if ack.StatusCode != 200 {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete remediation, got error: %s", ack.Status))
		return
	}
}

func (r *AWSResponseAckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("cloud_account_id"), req, resp)
}

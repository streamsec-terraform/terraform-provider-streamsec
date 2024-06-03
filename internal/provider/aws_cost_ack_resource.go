package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
var _ resource.Resource = &AWSCostAckResource{}
var _ resource.ResourceWithImportState = &AWSCostAckResource{}

func NewAWSCostAckResource() resource.Resource {
	return &AWSCostAckResource{}
}

type CostRequestBody struct {
	Operaion        string `json:"operation"`
	TemplateVersion int    `json:"template_version"`
	Status          string `json:"status"`
	Details         string `json:"details"`
	RoleARN         string `json:"role_arn"`
	BucketARN       string `json:"bucket_arn"`
	CURPrefix       string `json:"cur_prefix"`
	ExternalID      string `json:"external_id"`
}

type AWSCostAckResource struct {
	client *client.Client
}
type AWSCostAckResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	CloudAccountID           types.String `tfsdk:"cloud_account_id"`
	RoleARN                  types.String `tfsdk:"role_arn"`
	ExternalID               types.String `tfsdk:"external_id"`
	BucketARN                types.String `tfsdk:"bucket_arn"`
	CURPrefix                types.String `tfsdk:"cur_prefix"`
	StreamsecCollectionToken types.String `tfsdk:"streamsec_collection_token"`
}

func (r *AWSCostAckResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_cost_ack"
}

func (r *AWSCostAckResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "AWSCostAck resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The internal ID of the account.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
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
				Description: "The role_arn to access CUR bucket.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"external_id": schema.StringAttribute{
				Description: "The external ID.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"bucket_arn": schema.StringAttribute{
				Description: "The bucket arn.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cur_prefix": schema.StringAttribute{
				Description: "The CUR prefix.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
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

func (r *AWSCostAckResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AWSCostAckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AWSCostAckResourceModel

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
				cost {
					status
					role_arn
					external_id
					bucket_arn
					cur_prefix
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
			data.ID = types.StringValue(account["_id"].(string))
			data.StreamsecCollectionToken = types.StringValue(account["lightlytics_collection_token"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get account, account with cloud_account_id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	body := CostRequestBody{
		TemplateVersion: 1,
		Operaion:        "Create",
		Details:         "",
		Status:          "success",
		RoleARN:         data.RoleARN.ValueString(),
		ExternalID:      data.ExternalID.ValueString(),
		BucketARN:       data.BucketARN.ValueString(),
		CURPrefix:       data.CURPrefix.ValueString(),
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		fmt.Printf("Error marshalling JSON: %s\n", err)
		return
	}

	url := fmt.Sprintf("https://%s/api/v1/collection/cost/cft", r.client.Host)

	ackReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	ackReq.Header.Set("X-Lightlytics-Token", data.StreamsecCollectionToken.ValueString())

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

func (r *AWSCostAckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AWSCostAckResourceModel

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
				cost {
					status
					role_arn
					external_id
					bucket_arn
					cur_prefix
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
			data.ID = types.StringValue(account["_id"].(string))
			data.StreamsecCollectionToken = types.StringValue(account["lightlytics_collection_token"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get account, account with cloud_account_id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSCostAckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AWSCostAckResourceModel
	var state AWSCostAckResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSCostAckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AWSCostAckResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	body := CostRequestBody{
		Operaion:        "Delete",
		TemplateVersion: 1,
		Details:         "",
		Status:          "success",
		RoleARN:         data.RoleARN.ValueString(),
		ExternalID:      data.ExternalID.ValueString(),
		BucketARN:       data.BucketARN.ValueString(),
		CURPrefix:       data.CURPrefix.ValueString(),
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		fmt.Printf("Error marshalling JSON: %s\n", err)
		return
	}

	url := fmt.Sprintf("https://%s/api/v1/collection/cost/cft", r.client.Host)

	ackReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	ackReq.Header.Set("X-Lightlytics-Token", data.StreamsecCollectionToken.ValueString())

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
}

func (r *AWSCostAckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("cloud_account_id"), req, resp)
}

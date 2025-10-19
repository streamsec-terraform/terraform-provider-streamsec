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
var _ resource.Resource = &GCPResponseAckResource{}
var _ resource.ResourceWithImportState = &GCPResponseAckResource{}

func NewGCPResponseAckResource() resource.Resource {
	return &GCPResponseAckResource{}
}

type GCPRemediationRequestBody struct {
	AccountId       string   `json:"gcp_project_id"`
	TemplateVersion string   `json:"template_version"`
	RunbookList     []string `json:"runbook_list"`
	Location        string   `json:"location"`
}

type GCPResponseAckResource struct {
	client *client.Client
}
type GCPResponseAckResourceModel struct {
	ID              types.String `tfsdk:"id"`
	CloudAccountID  types.String `tfsdk:"cloud_account_id"`
	AccountToken    types.String `tfsdk:"account_token"`
	TemplateVersion types.String `tfsdk:"template_version"`
	RunbookList     types.List   `tfsdk:"runbook_list"`
	Location        types.String `tfsdk:"location"`
}

func (r *GCPResponseAckResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gcp_response_ack"
}

func (r *GCPResponseAckResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "GCPResponseAck resource",

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
				// add validator to check if the cloud_account_id is a valid GCP project ID
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9][a-z0-9-]{4,28}[a-z0-9]$`), "The cloud account ID must be a valid GCP project ID."),
				},
			},
			"runbook_list": schema.ListAttribute{
				ElementType: types.StringType,
				Description: "The runbook list.",
				Required:    true,
			},
			"location": schema.StringAttribute{
				Description: "The location.",
				Required:    true,
			},
			"template_version": schema.StringAttribute{
				Description: "The template version.",
				Required:    true,
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

func (r *GCPResponseAckResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *GCPResponseAckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GCPResponseAckResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	query := `
		query {
			accounts {
				_id
				cloud_account_id
				account_token
				remediation {
					status
					runbook_list
					location
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
			if account["remediation"] != nil && account["remediation"].(map[string]interface{})["status"] != nil {
				if account["remediation"].(map[string]interface{})["status"].(string) == "READY" {
					resp.Diagnostics.AddError("Client Error", "Account remediation is already enabled")
					return
				}
			}
			data.ID = types.StringValue(account["_id"].(string))
			data.AccountToken = types.StringValue(account["account_token"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get account, account with cloud_account_id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	body := GCPRemediationRequestBody{
		AccountId:       data.CloudAccountID.ValueString(),
		TemplateVersion: data.TemplateVersion.ValueString(),
		RunbookList:     utils.ConvertToStringSlice(data.RunbookList.Elements()),
		Location:        data.Location.ValueString(),
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		fmt.Printf("Error marshalling JSON: %s\n", err)
		return
	}

	url := fmt.Sprintf("https://%s/gcp/remediation-acknowledge", r.client.Host)

	ackReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	ackReq.Header.Set("Authorization", "Bearer "+data.AccountToken.ValueString())
	ackReq.Header.Set("Content-Type", "application/json")

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

func (r *GCPResponseAckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data GCPResponseAckResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	query := `
		query {
			accounts {
				_id
				cloud_account_id
				account_token
				remediation {
					status
					runbook_list
					location
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
			data.AccountToken = types.StringValue(account["account_token"].(string))
			data.RunbookList = utils.ConvertInterfaceToTypesList((account["remediation"].(map[string]interface{})["runbook_list"].([]interface{})))
			data.Location = types.StringValue(account["remediation"].(map[string]interface{})["location"].(string))

			accountFound = true
		}
	}

	if !accountFound {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GCPResponseAckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GCPResponseAckResourceModel
	var state GCPResponseAckResourceModel

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
				cloud_account_id
				account_token
				remediation {
					status
					runbook_list
					location
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
			data.AccountToken = types.StringValue(account["account_token"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get account, account with cloud_account_id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	body := GCPRemediationRequestBody{
		AccountId:       data.CloudAccountID.ValueString(),
		Location:        data.Location.ValueString(),
		TemplateVersion: data.TemplateVersion.ValueString(),
		RunbookList:     utils.ConvertToStringSlice(data.RunbookList.Elements()),
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		fmt.Printf("Error marshalling JSON: %s\n", err)
		return
	}

	url := fmt.Sprintf("https://%s/gcp/remediation-acknowledge", r.client.Host)

	ackReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	ackReq.Header.Set("Authorization", "Bearer "+data.AccountToken.ValueString())
	ackReq.Header.Set("Content-Type", "application/json")

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
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update account, got error: %s", ack.Status))
		return
	}

	tflog.Debug(ctx, fmt.Sprintf("Response: %v", res))

	// Write logs using the tflog package
	tflog.Trace(ctx, "updated a resource")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GCPResponseAckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data GCPResponseAckResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	url := fmt.Sprintf("https://%s/api/accounts/accounts/remediation/%s", r.client.Host, data.CloudAccountID.ValueString())

	ackReq, err := http.NewRequest("DELETE", url, nil)
	ackReq.Header.Set("Authorization", "Bearer "+data.AccountToken.ValueString())

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

func (r *GCPResponseAckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("cloud_account_id"), req, resp)
}

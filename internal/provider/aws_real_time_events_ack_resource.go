package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
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
var _ resource.Resource = &AWSRealTimeEventsAckResource{}
var _ resource.ResourceWithImportState = &AWSRealTimeEventsAckResource{}

func NewAWSRealTimeEventsAckResource() resource.Resource {
	return &AWSRealTimeEventsAckResource{}
}

type CFTEventRequestBody struct {
	AccountId       string `json:"AccountId"`
	Region          string `json:"Region"`
	TemplateVersion string `json:"TemplateVersion"`
	Operaion        string `json:"operation"`
}

type AWSRealTimeEventsAckResource struct {
	client *client.Client
}
type AWSRealTimeEventsAckResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	CloudAccountID           types.String `tfsdk:"cloud_account_id"`
	Region                   types.String `tfsdk:"region"`
	StreamsecCollectionToken types.String `tfsdk:"streamsec_collection_token"`
}

func (r *AWSRealTimeEventsAckResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_real_time_events_ack"
}

func (r *AWSRealTimeEventsAckResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "AWSRealTimeEventsAck resource",

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

func (r *AWSRealTimeEventsAckResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AWSRealTimeEventsAckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AWSRealTimeEventsAckResourceModel

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
				realtime_regions {
					region_name
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
			if account["realtime_regions"] != nil {
				regions := account["realtime_regions"].([]interface{})
				for _, region := range regions {
					if region.(map[string]interface{})["region_name"].(string) == data.Region.ValueString() {
						resp.Diagnostics.AddError("Region already exists", "The specified region is already enabled for real-time events. Please import using terraform import.")
						return
					}
				}
			}
			data.ID = types.StringValue(account["_id"].(string))
			data.StreamsecCollectionToken = types.StringValue(account["lightlytics_collection_token"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get account, account with cloud_account_id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	body := CFTEventRequestBody{
		AccountId:       data.CloudAccountID.ValueString(),
		Region:          data.Region.ValueString(),
		TemplateVersion: "1",
		Operaion:        "Create",
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		fmt.Printf("Error marshalling JSON: %s\n", err)
		return
	}

	url := fmt.Sprintf("https://%s/api/v1/collection/cloudtrail/cft-event", r.client.Host)

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

func (r *AWSRealTimeEventsAckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AWSRealTimeEventsAckResourceModel

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
				realtime_regions {
					region_name
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
	regionFound := false

	for _, acc := range accounts {

		account := acc.(map[string]interface{})
		if account["cloud_account_id"].(string) == data.CloudAccountID.ValueString() {
			if account["realtime_regions"] != nil {
				regions := account["realtime_regions"].([]interface{})
				tflog.Debug(ctx, fmt.Sprintf("Regions: %v", regions))
				for _, region := range regions {
					if region.(map[string]interface{})["region_name"].(string) == data.Region.ValueString() {
						regionFound = true
					}
				}
			}
			if !regionFound {
				resp.State.RemoveResource(ctx)
				return
			}
			data.ID = types.StringValue(account["_id"].(string))
			data.StreamsecCollectionToken = types.StringValue(account["lightlytics_collection_token"].(string))
			accountFound = true
		}
	}

	if !accountFound {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSRealTimeEventsAckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AWSRealTimeEventsAckResourceModel
	var state AWSRealTimeEventsAckResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSRealTimeEventsAckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AWSRealTimeEventsAckResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	body := CFTEventRequestBody{
		AccountId:       data.CloudAccountID.ValueString(),
		Region:          data.Region.ValueString(),
		TemplateVersion: "1",
		Operaion:        "Delete",
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		fmt.Printf("Error marshalling JSON: %s\n", err)
		return
	}

	url := fmt.Sprintf("https://%s/api/v1/collection/cloudtrail/cft-event", r.client.Host)

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

func (r *AWSRealTimeEventsAckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idParts := strings.Split(req.ID, ",")
	if len(idParts) != 2 || idParts[0] == "" || idParts[1] == "" {
		resp.Diagnostics.AddError(
			"Unexpected Import Identifier",
			fmt.Sprintf("Expected import identifier with format: account_id,region. Got: %q", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("cloud_account_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("region"), idParts[1])...)
}

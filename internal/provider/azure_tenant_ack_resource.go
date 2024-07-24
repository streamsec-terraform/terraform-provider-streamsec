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
	"terraform-provider-streamsec/internal/utils"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
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
var _ resource.Resource = &AzureTenantAckResource{}
var _ resource.ResourceWithImportState = &AzureTenantAckResource{}

func NewAzureTenantAckResource() resource.Resource {
	return &AzureTenantAckResource{}
}

type AzureTenantAckResource struct {
	client *client.Client
}
type AzureTenantAckResourceModel struct {
	ID             types.String `tfsdk:"id"`
	CloudAccountID types.String `tfsdk:"tenant_id"`
	ClientID       types.String `tfsdk:"client_id"`
	ClientSecret   types.String `tfsdk:"client_secret"`
	Subscriptions  types.List   `tfsdk:"subscriptions"`
	AccountToken   types.String `tfsdk:"account_token"`
}

type AzureAckRequestBody struct {
	AccountType   string `json:"account_type"`
	TenantID      string `json:"tenant_id"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	Subscriptions string `json:"subscriptions"`
}

func (r *AzureTenantAckResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_azure_tenant_ack"
}

func (r *AzureTenantAckResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "AzureTenantAck resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The internal ID of the tenant.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"tenant_id": schema.StringAttribute{
				Description: "The Azure tenant ID.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^[0-9a-z-]{36}$`), "Azure tenant ID must be a 36-character string with lowercase letters, numbers, and hyphens."),
				},
			},
			"client_id": schema.StringAttribute{
				Description: "The client ID.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^[0-9a-z-]{36}$`), "Client ID must be a 36-character string with lowercase letters, numbers, and hyphens."),
				},
			},
			"client_secret": schema.StringAttribute{
				Description: "The client secret.",
				Required:    true,
				Sensitive:   true,
			},
			"subscriptions": schema.ListAttribute{
				ElementType: types.StringType,
				Description: "The subscriptions integrated",
				Required:    true,
				Validators: []validator.List{
					listvalidator.All(
						listvalidator.ValueStringsAre(
							stringvalidator.RegexMatches(regexp.MustCompile(`^[0-9a-z-]{36}$`), "Subscription ID must be a 36-character string with lowercase letters, numbers, and hyphens."),
						),
					),
				},
			},
			"account_token": schema.StringAttribute{
				Description: "The collection token.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *AzureTenantAckResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *AzureTenantAckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AzureTenantAckResourceModel

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
				account_token
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
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get tenant, tenant with id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Tenant found: %v", data))

	body := AzureAckRequestBody{
		AccountType:   "Azure",
		TenantID:      data.CloudAccountID.ValueString(),
		ClientID:      data.ClientID.ValueString(),
		ClientSecret:  data.ClientSecret.ValueString(),
		Subscriptions: strings.Join(utils.ConvertToStringSlice(data.Subscriptions.Elements()), ","),
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		fmt.Printf("Error marshalling JSON: %s\n", err)
		return
	}

	url := fmt.Sprintf("https://%s/azure/account-acknowledge", r.client.Host)

	ackReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	ackReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", data.AccountToken.ValueString()))

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

func (r *AzureTenantAckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AzureTenantAckResourceModel

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
				client_id
				subscriptions {
					id
				}
				account_token
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
			if account["subscriptions"] != nil {
				subscriptions := account["subscriptions"].([]interface{})
				// create a list of subscription IDs
				var subscriptionIDs []string
				for _, sub := range subscriptions {
					subscriptionIDs = append(subscriptionIDs, sub.(map[string]interface{})["id"].(string))
				}
				data.Subscriptions = utils.ConvertStringsArrayToTypesList(subscriptionIDs)
			} else {
				data.Subscriptions = types.ListValueMust(types.StringType, []attr.Value{})
			}
			data.AccountToken = types.StringValue(account["account_token"].(string))
			data.ID = types.StringValue(account["_id"].(string))

			accountFound = true
		}
	}

	if !accountFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get account, account with cloud_account_id: %s not found in Stream.Security API.", data.CloudAccountID.ValueString()))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AzureTenantAckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AzureTenantAckResourceModel
	var state AzureTenantAckResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// check if there was a change in display_name
	if !utils.EqualListValues(data.Subscriptions, state.Subscriptions) || data.ClientID != state.ClientID || data.ClientSecret != state.ClientSecret {
		query := `
			mutation UpdateAccount($id: ID!, $account: AccountUpdateInput) {
				updateAccount(id: $id, account: $account) {
					_id
				}
			}`

		variables := map[string]interface{}{
			"id": data.ID.ValueString(),
			"account": map[string]interface{}{
				"subscriptions": utils.ConvertToStringSlice(data.Subscriptions.Elements()),
				"client_id":     data.ClientID.ValueString(),
				"client_secret": data.ClientSecret.ValueString(),
			},
		}

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

func (r *AzureTenantAckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AzureTenantAckResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *AzureTenantAckResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("tenant_id"), req, resp)
}

package provider

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"terraform-provider-streamsec/internal/client"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &AzureVolumeScannerAckResource{}
var _ resource.ResourceWithConfigure = &AzureVolumeScannerAckResource{}

// An Azure subscription id, as Stream Security matches it whole.
var azureSubscriptionPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func NewAzureVolumeScannerAckResource() resource.Resource {
	return &AzureVolumeScannerAckResource{}
}

// AzureVolumeScannerAckResource tells Stream Security that the Terraform
// volume-scanner module is installed in an Azure subscription, as the ARM
// template's deployment script does.
type AzureVolumeScannerAckResource struct {
	client *client.Client
}

type AzureVolumeScannerAckResourceModel struct {
	TenantID       types.String `tfsdk:"tenant_id"`
	SubscriptionID types.String `tfsdk:"subscription_id"`
	ModuleVersion  types.String `tfsdk:"module_version"`
	ResourceGroup  types.String `tfsdk:"resource_group"`
}

type azureVolumeScannerAckRequest struct {
	SubscriptionID string `json:"subscription_id"`
	Status         string `json:"status"`
	ModuleVersion  string `json:"module_version"`
	ResourceGroup  string `json:"resource_group,omitempty"`
}

func azureVolumeScannerHints(tenantID, subscriptionID string) statusHints {
	return statusHints{
		http.StatusBadRequest: "Stream Security refused subscription_id or module_version: subscription_id must be an Azure subscription id, " +
			"and module_version a version, such as 1.6.0 (a leading v and -pre or +build suffixes are allowed), of at most 64 characters.",
		http.StatusForbidden: fmt.Sprintf("The token has no access to Azure tenant %s%s.", tenantID, trialExpiredHint),
		http.StatusNotFound: fmt.Sprintf("Either tenant %s is not in Stream Security, or subscription %s is not one of its subscriptions: "+
			"tenant_id takes the Stream Security id of the tenant (streamsec_azure_tenant.<name>.id), not the Azure tenant id.",
			tenantID, subscriptionID),
		http.StatusConflict: fmt.Sprintf("Azure tenant %s is not connected to Stream Security yet, or is being deleted: "+
			"the scanner is acknowledged only after the tenant's own (streamsec_azure_tenant_ack).", tenantID),
	}
}

func (r *AzureVolumeScannerAckResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_azure_volume_scanner_ack"
}

func (r *AzureVolumeScannerAckResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Acknowledges a Terraform install of the agentless volume scanner in one Azure subscription, " +
			"as the ARM template's deployment does. Stream Security then accepts the scanner's reports with the subscription's " +
			"collection token (`streamsec_azure_volume_scanner`). Destroying it reports the scanner uninstalled.",

		Attributes: map[string]schema.Attribute{
			"tenant_id": schema.StringAttribute{
				MarkdownDescription: "The Stream Security id of the Azure tenant (`streamsec_azure_tenant.<name>.id`). Changing it replaces the resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"subscription_id": schema.StringAttribute{
				MarkdownDescription: "The Azure subscription the scanner is installed in, one of the tenant's subscriptions in Stream Security. " +
					"Changing it replaces the resource.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(azureSubscriptionPattern, "must be an Azure subscription id"),
				},
			},
			"module_version": schema.StringAttribute{
				MarkdownDescription: "The version of the Terraform module that installed the scanner: `MAJOR.MINOR.PATCH`, with an optional " +
					"leading `v` and `-pre` or `+build` suffixes, at most 64 characters.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(64),
					stringvalidator.RegexMatches(moduleVersionPattern, "must be a version such as 1.6.0 (a leading v and -pre or +build suffixes are allowed)"),
				},
			},
			"resource_group": schema.StringAttribute{
				MarkdownDescription: "The resource group the scanner's resources are in, shown in Stream Security.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 90),
				},
			},
		},
	}
}

func (r *AzureVolumeScannerAckResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

func (r *AzureVolumeScannerAckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AzureVolumeScannerAckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.acknowledge(ctx, data, "deployed"); err != nil {
		addAPIError(&resp.Diagnostics, "Unable to acknowledge the volume scanner", err, r.hints(data))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read keeps the state: the acknowledgement has nothing to read back.
func (r *AzureVolumeScannerAckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *AzureVolumeScannerAckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AzureVolumeScannerAckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.acknowledge(ctx, data, "deployed"); err != nil {
		addAPIError(&resp.Diagnostics, "Unable to acknowledge the volume scanner", err, r.hints(data))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AzureVolumeScannerAckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AzureVolumeScannerAckResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.acknowledge(ctx, data, "uninstalled"); err != nil {
		addDeleteError(&resp.Diagnostics, "Unable to acknowledge the volume scanner uninstall", "Volume scanner uninstall not recorded",
			err, r.hints(data), uninstallGoneStatuses)
	}
}

func (r *AzureVolumeScannerAckResource) hints(data AzureVolumeScannerAckResourceModel) statusHints {
	return azureVolumeScannerHints(data.TenantID.ValueString(), data.SubscriptionID.ValueString())
}

func (r *AzureVolumeScannerAckResource) acknowledge(ctx context.Context, data AzureVolumeScannerAckResourceModel, status string) error {
	return r.client.DoRESTRequest(ctx, http.MethodPut,
		client.AccountsPath(data.TenantID.ValueString(), "azure-scanner-terraform-acknowledge"),
		azureVolumeScannerAckRequest{
			SubscriptionID: data.SubscriptionID.ValueString(),
			Status:         status,
			ModuleVersion:  data.ModuleVersion.ValueString(),
			ResourceGroup:  data.ResourceGroup.ValueString(),
		}, nil)
}

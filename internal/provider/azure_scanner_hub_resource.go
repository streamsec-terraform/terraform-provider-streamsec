package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"terraform-provider-streamsec/internal/client"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &AzureScannerHubResource{}
var _ resource.ResourceWithConfigure = &AzureScannerHubResource{}
var _ resource.ResourceWithImportState = &AzureScannerHubResource{}
var _ resource.ResourceWithValidateConfig = &AzureScannerHubResource{}

func NewAzureScannerHubResource() resource.Resource {
	return &AzureScannerHubResource{}
}

// AzureScannerHubResource makes an Azure subscription's agentless scanner a hub that also
// scans other subscriptions of its tenant. An Azure account is a whole tenant, so
// the Stream API names a subscription "<tenant account id>:<subscription id>".
type AzureScannerHubResource struct {
	client *client.Client
}

type AzureScannerHubResourceModel struct {
	AccountID       types.String `tfsdk:"account_id"`
	SubscriptionID  types.String `tfsdk:"subscription_id"`
	Coverage        types.String `tfsdk:"coverage"`
	SubscriptionIDs types.Set    `tfsdk:"subscription_ids"`
}

var azureTenantKind = accountKind{
	accountType:  "Azure",
	noun:         "an Azure tenant",
	resourceType: "streamsec_azure_scanner_hub",
	ref:          "streamsec_azure_tenant.<name>.id",
	nativeID:     "the Azure tenant id",
}

var azureScannerHubHints = statusHints{
	http.StatusBadRequest: "Stream Security refused the coverage: subscription_id must be a subscription of the tenant with a scanner, " +
		"and each of subscription_ids a subscription of the same tenant other than a hub. account_id takes the tenant's " +
		"Stream Security id (streamsec_azure_tenant.<name>.id), not the Azure tenant id.",
	http.StatusForbidden: scannerHubForbiddenHint,
	http.StatusNotFound: "subscription_id is not a subscription of the tenant in Stream Security: account_id takes the tenant's " +
		"Stream Security id (streamsec_azure_tenant.<name>.id), and the subscription must be one of its subscriptions.",
	http.StatusConflict: `Another subscription's scanner already scans all subscriptions of the tenant (only one hub per tenant may cover "all"), ` +
		"or one of subscription_ids is already scanned by another hub. Change that hub's coverage first. " +
		`A subscription may also still be pinned to another hub that scanned it while its coverage was "selected", after that hub ` +
		`switched to "all": to release it, set that hub to "selected" without the subscription, apply, then set it back to "all".`,
}

func (r *AzureScannerHubResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_azure_scanner_hub"
}

func (r *AzureScannerHubResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Makes an Azure subscription's agentless volume scanner a hub: it also scans other subscriptions of the tenant, " +
			"as its own managed identity, where that identity holds the scanned-subscriptions role (the azure-tenant module's " +
			"`modules/volume-scanner-hub-access`, or the console's access template, grants it). " +
			"Destroying it turns the hub off: each subscription goes back to its own scanner.\n\n" +
			"A subscription a hub scanned while its coverage was `selected` stays pinned to that hub after it switches to `all`, " +
			"and another hub can't select it (Stream Security answers 409). To release it, set that hub to `selected` " +
			"without the subscription, apply, then set it back to `all`.",

		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				MarkdownDescription: "The Stream Security id of the Azure tenant (`streamsec_azure_tenant.<name>.id`), not the Azure tenant id. " +
					"Changing it replaces the resource.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(streamsecAccountIDPattern,
						"must be the Stream Security id of the tenant (streamsec_azure_tenant.<name>.id), not the Azure tenant id"),
				},
			},
			"subscription_id": schema.StringAttribute{
				MarkdownDescription: "The hub's subscription, where its scanner runs, in lowercase. Changing it replaces the resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(azureSubscriptionPattern, "must be an Azure subscription id, in lowercase"),
				},
			},
			"coverage": scannerHubCoverageAttribute("Which subscriptions the hub scans: `all` (every other subscription of the tenant " +
				"without a scanner of its own and not scanned by another hub) or `selected` (the subscriptions in `subscription_ids`). " +
				"Only one hub per tenant may cover `all`."),
			"subscription_ids": schema.SetAttribute{
				MarkdownDescription: "The subscriptions of the tenant the hub scans, in lowercase. Required when `coverage` is `selected` " +
					"(an empty set scans none); must be unset or empty when `coverage` is `all`.",
				ElementType: types.StringType,
				Optional:    true,
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.RegexMatches(azureSubscriptionPattern, "must be an Azure subscription id, in lowercase")),
				},
			},
		},
	}
}

func (r *AzureScannerHubResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data AzureScannerHubResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateScannerHubCoverage(data.Coverage, data.SubscriptionIDs, "subscription_ids", "subscriptions", &resp.Diagnostics)
}

func (r *AzureScannerHubResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

// azureSubscriptionKey is how the Stream API names a subscription of a tenant.
func azureSubscriptionKey(accountID, subscriptionID string) string {
	return accountID + ":" + subscriptionID
}

func (data AzureScannerHubResourceModel) hubKey() string {
	return azureSubscriptionKey(data.AccountID.ValueString(), data.SubscriptionID.ValueString())
}

func (r *AzureScannerHubResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AzureScannerHubResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	requireAccountKind(ctx, r.client, data.AccountID.ValueString(), azureTenantKind, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.setCoverage(ctx, data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AzureScannerHubResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AzureScannerHubResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hubKey := data.hubKey()
	coverage, keys, found := readScannerHub(ctx, r.client, azureTenantKind.accountType, hubKey, azureScannerHubHints, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		tflog.Info(ctx, "Scanner hub no longer covers any subscription, removing it from state", map[string]interface{}{"hub": hubKey})
		resp.State.RemoveResource(ctx)
		return
	}

	data.Coverage = types.StringValue(coverage)
	if coverage == "selected" {
		prefix := data.AccountID.ValueString() + ":"
		ids := []attr.Value{}
		for _, key := range keys {
			// A hub covers its own tenant's subscriptions only.
			if subscription, ok := strings.CutPrefix(key, prefix); ok {
				ids = append(ids, types.StringValue(subscription))
			}
		}
		data.SubscriptionIDs = types.SetValueMust(types.StringType, ids)
	} else if len(data.SubscriptionIDs.Elements()) > 0 {
		// An "all" hub has no subscription_ids; the subscriptions pinned to it are not them.
		data.SubscriptionIDs = types.SetNull(types.StringType)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AzureScannerHubResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AzureScannerHubResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.setCoverage(ctx, data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AzureScannerHubResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AzureScannerHubResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	turnOffScannerHub(ctx, r.client, data.hubKey(), azureScannerHubHints, &resp.Diagnostics)
}

// ImportState takes "<account_id>:<subscription_id>", the hub's name in Stream Security.
func (r *AzureScannerHubResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	accountID, subscriptionID, ok := strings.Cut(req.ID, ":")
	if !ok || accountID == "" || subscriptionID == "" {
		resp.Diagnostics.AddError("Invalid import ID",
			fmt.Sprintf("Expected <account_id>:<subscription_id>, the tenant's Stream Security id and the hub's subscription, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("account_id"), accountID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("subscription_id"), subscriptionID)...)
}

func (r *AzureScannerHubResource) setCoverage(ctx context.Context, data AzureScannerHubResourceModel, diags *diag.Diagnostics) {
	subscriptions := []string{}
	if !data.SubscriptionIDs.IsNull() && !data.SubscriptionIDs.IsUnknown() {
		diags.Append(data.SubscriptionIDs.ElementsAs(ctx, &subscriptions, false)...)
		if diags.HasError() {
			return
		}
	}
	keys := make([]string, len(subscriptions))
	for i, subscription := range subscriptions {
		keys[i] = azureSubscriptionKey(data.AccountID.ValueString(), subscription)
	}
	putScannerHub(ctx, r.client, data.hubKey(), data.Coverage.ValueString(), keys, azureScannerHubHints, diags)
}

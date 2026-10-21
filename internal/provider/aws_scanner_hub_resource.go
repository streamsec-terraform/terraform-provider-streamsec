package provider

import (
	"context"
	"net/http"
	"sort"

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

var _ resource.Resource = &AWSScannerHubResource{}
var _ resource.ResourceWithConfigure = &AWSScannerHubResource{}
var _ resource.ResourceWithImportState = &AWSScannerHubResource{}
var _ resource.ResourceWithValidateConfig = &AWSScannerHubResource{}

func NewAWSScannerHubResource() resource.Resource {
	return &AWSScannerHubResource{}
}

// AWSScannerHubResource makes an AWS account's agentless scanner a hub that also
// scans other AWS accounts (DEV-23499), through ms_accounts' scanner-hub routes.
type AWSScannerHubResource struct {
	client *client.Client
}

type AWSScannerHubResourceModel struct {
	AccountID  types.String `tfsdk:"account_id"`
	Coverage   types.String `tfsdk:"coverage"`
	AccountIDs types.Set    `tfsdk:"account_ids"`
}

type scannerHubRequest struct {
	Coverage string   `json:"coverage"`
	Accounts []string `json:"accounts"`
}

// The part of GET scanner-coverage this resource reads.
type scannerCoverage struct {
	Hubs []struct {
		HubAccountID string `json:"hub_account_id"`
		Coverage     string `json:"coverage"`
	} `json:"hubs"`
	Assignments []struct {
		AccountID    string `json:"account_id"`
		HubAccountID string `json:"hub_account_id"`
	} `json:"assignments"`
}

var scannerHubHints = statusHints{
	http.StatusBadRequest: "Stream Security refused the coverage: account_id must be a connected AWS account, and each of " +
		"account_ids a connected AWS account other than a hub. Both take Stream Security account ids " +
		"(streamsec_aws_account.<name>.id), not 12-digit AWS account ids.",
	http.StatusForbidden: "Changing what a scanner covers needs a token with access to all accounts.",
	http.StatusNotFound:  "account_id is not an account in Stream Security: it takes the Stream Security account id (streamsec_aws_account.<name>.id).",
	http.StatusConflict: "Another account's scanner already scans all AWS accounts, or one of account_ids is already " +
		"scanned by another hub. Change that hub's coverage first.",
}

func (r *AWSScannerHubResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_scanner_hub"
}

func (r *AWSScannerHubResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Makes an AWS account's agentless volume scanner a hub: it also scans other AWS accounts in its regions, " +
			"through the `StreamScannerSpokeRole` each of them creates (see the `streamsec_aws_scanner_hub_access` data source). " +
			"Destroying it turns the hub off: each account goes back to its own scanner.",

		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				MarkdownDescription: "The Stream Security id of the hub's AWS account (`streamsec_aws_account.<name>.id`). Changing it replaces the resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"coverage": schema.StringAttribute{
				MarkdownDescription: "Which accounts the hub scans: `all` (every other AWS account without a scanner of its own in the region " +
					"and not scanned by another hub) or `selected` (the accounts in `account_ids`). Only one hub may cover `all`.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("all", "selected"),
				},
			},
			"account_ids": schema.SetAttribute{
				MarkdownDescription: "The Stream Security ids of the AWS accounts the hub scans. Required when `coverage` is `selected` " +
					"(an empty set scans none); must be unset or empty when `coverage` is `all`.",
				ElementType: types.StringType,
				Optional:    true,
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
		},
	}
}

func (r *AWSScannerHubResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data AWSScannerHubResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.Coverage.IsNull() || data.Coverage.IsUnknown() {
		return
	}
	switch data.Coverage.ValueString() {
	case "selected":
		if data.AccountIDs.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("account_ids"), "Missing account_ids",
				`account_ids is required when coverage is "selected": the accounts the hub scans (an empty set scans none).`)
		}
	case "all":
		if !data.AccountIDs.IsUnknown() && len(data.AccountIDs.Elements()) > 0 {
			resp.Diagnostics.AddAttributeError(path.Root("account_ids"), "Unexpected account_ids",
				`account_ids must be unset or empty when coverage is "all": the hub scans every account no other scanner does.`)
		}
	}
}

func (r *AWSScannerHubResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

func (r *AWSScannerHubResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AWSScannerHubResourceModel
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

func (r *AWSScannerHubResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data AWSScannerHubResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var coverage scannerCoverage
	err := r.client.DoRESTRequest(ctx, http.MethodGet, client.AccountsPath("scanner-coverage")+"?cloud=AWS", nil, &coverage)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Unable to read the scanner coverage", err, scannerHubHints)
		return
	}

	hubID := data.AccountID.ValueString()
	found := false
	for _, hub := range coverage.Hubs {
		if hub.HubAccountID == hubID {
			data.Coverage = types.StringValue(hub.Coverage)
			found = true
			break
		}
	}
	if !found {
		tflog.Info(ctx, "Scanner hub no longer covers any account, removing it from state", map[string]interface{}{"account_id": hubID})
		resp.State.RemoveResource(ctx)
		return
	}

	if data.Coverage.ValueString() == "selected" {
		ids := []attr.Value{}
		for _, assignment := range coverage.Assignments {
			if assignment.HubAccountID == hubID {
				ids = append(ids, types.StringValue(assignment.AccountID))
			}
		}
		data.AccountIDs = types.SetValueMust(types.StringType, ids)
	} else if len(data.AccountIDs.Elements()) > 0 {
		// An "all" hub has no account_ids; the accounts pinned to it are not them.
		data.AccountIDs = types.SetNull(types.StringType)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSScannerHubResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AWSScannerHubResourceModel
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

func (r *AWSScannerHubResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AWSScannerHubResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DoRESTRequest(ctx, http.MethodPut, client.AccountsPath(data.AccountID.ValueString(), "scanner-hub"),
		map[string]string{"coverage": "none"}, nil)
	if err != nil {
		addAPIError(&resp.Diagnostics, "Unable to turn off the scanner hub", err, scannerHubHints)
	}
}

func (r *AWSScannerHubResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("account_id"), req, resp)
}

func (r *AWSScannerHubResource) setCoverage(ctx context.Context, data AWSScannerHubResourceModel, diags *diag.Diagnostics) {
	accounts := []string{}
	if !data.AccountIDs.IsNull() && !data.AccountIDs.IsUnknown() {
		diags.Append(data.AccountIDs.ElementsAs(ctx, &accounts, false)...)
		if diags.HasError() {
			return
		}
	}
	if accounts == nil {
		accounts = []string{}
	}
	sort.Strings(accounts)

	err := r.client.DoRESTRequest(ctx, http.MethodPut, client.AccountsPath(data.AccountID.ValueString(), "scanner-hub"),
		scannerHubRequest{Coverage: data.Coverage.ValueString(), Accounts: accounts}, nil)
	if err != nil {
		addAPIError(diags, "Unable to set the scanner hub's coverage", err, scannerHubHints)
	}
}

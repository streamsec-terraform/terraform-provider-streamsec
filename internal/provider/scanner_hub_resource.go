package provider

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

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

var _ resource.Resource = &ScannerHubResource{}
var _ resource.ResourceWithConfigure = &ScannerHubResource{}
var _ resource.ResourceWithImportState = &ScannerHubResource{}
var _ resource.ResourceWithValidateConfig = &ScannerHubResource{}

// ScannerHubResource makes an AWS account's or a GCP project's agentless scanner a hub
// that also scans other accounts of its cloud (DEV-23499), through ms_accounts'
// scanner-hub routes. An Azure hub is a subscription: AzureScannerHubResource.
type ScannerHubResource struct {
	client *client.Client
	cloud  scannerHubCloud
}

// scannerHubCloud is what an AWS hub and a GCP hub tell apart.
type scannerHubCloud struct {
	account               accountKind
	description           string
	accountIDDescription  string
	coverageDescription   string
	accountIDsDescription string
	hints                 statusHints
}

// accountKind is the Stream Security account a hub resource takes.
type accountKind struct {
	accountType  string // account_type, and scanner-coverage's cloud
	noun         string // "an AWS account"
	resourceType string // the hub resource's type, for messages
	ref          string // how a configuration names the account's Stream Security id
	nativeID     string // the cloud's own id, which account_id is not
}

type ScannerHubResourceModel struct {
	AccountID  types.String `tfsdk:"account_id"`
	Coverage   types.String `tfsdk:"coverage"`
	AccountIDs types.Set    `tfsdk:"account_ids"`
}

type scannerHubRequest struct {
	Coverage string   `json:"coverage"`
	Accounts []string `json:"accounts"`
}

// The part of GET scanner-coverage the hub resources read.
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

// Kept in the backend by design, and not in account_ids, so Read can't show it.
const pinnedAccountNote = `An account may also still be pinned to another hub that scanned it while its coverage was "selected", ` +
	`after that hub switched to "all": to release it, set that hub to "selected" without the account, apply, then set it back to "all".`

const pinnedAccountDescription = "An account a hub scanned while its coverage was `selected` stays pinned to that hub after it switches to `all`, " +
	"and another hub can't select it (Stream Security answers 409). To release it, set that hub to `selected` " +
	"without the account, apply, then set it back to `all`."

const scannerHubForbiddenHint = "Changing what a scanner covers needs a token with access to all accounts" + trialExpiredHint + "."

func (r *ScannerHubResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = r.cloud.account.resourceType
}

func (r *ScannerHubResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: r.cloud.description,

		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				MarkdownDescription: r.cloud.accountIDDescription,
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"coverage": scannerHubCoverageAttribute(r.cloud.coverageDescription),
			"account_ids": schema.SetAttribute{
				MarkdownDescription: r.cloud.accountIDsDescription,
				ElementType:         types.StringType,
				Optional:            true,
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
		},
	}
}

func scannerHubCoverageAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: description,
		Required:            true,
		Validators: []validator.String{
			stringvalidator.OneOf("all", "selected"),
		},
	}
}

func (r *ScannerHubResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data ScannerHubResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateScannerHubCoverage(data.Coverage, data.AccountIDs, "account_ids", "accounts", &resp.Diagnostics)
}

// validateScannerHubCoverage checks the covered set against coverage: required with
// "selected", unset or empty with "all". what names the set's members in messages.
func validateScannerHubCoverage(coverage types.String, ids types.Set, attribute, what string, diags *diag.Diagnostics) {
	if coverage.IsNull() || coverage.IsUnknown() {
		return
	}
	switch coverage.ValueString() {
	case "selected":
		if ids.IsNull() {
			diags.AddAttributeError(path.Root(attribute), "Missing "+attribute,
				fmt.Sprintf(`%s is required when coverage is "selected": the %s the hub scans (an empty set scans none).`, attribute, what))
		}
	case "all":
		if !ids.IsUnknown() && len(ids.Elements()) > 0 {
			diags.AddAttributeError(path.Root(attribute), "Unexpected "+attribute,
				fmt.Sprintf(`%s must be unset or empty when coverage is "all": the hub scans every %s no other scanner does.`,
					attribute, strings.TrimSuffix(what, "s")))
		}
	}
}

func (r *ScannerHubResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

func (r *ScannerHubResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ScannerHubResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	requireAccountKind(ctx, r.client, data.AccountID.ValueString(), r.cloud.account, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	r.setCoverage(ctx, data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// accountCheckTimeout bounds requireAccountKind's query: the GraphQL client has no timeout.
var accountCheckTimeout = time.Minute

// requireAccountKind refuses an account of another cloud. ms_accounts takes a hub's cloud
// from its account, so a GCP project's id would make a GCP hub, which an AWS hub's Read
// (its own cloud's hubs only) never finds and destroy never turns off. account_id replaces
// the resource, so Create is the only place it can change.
func requireAccountKind(ctx context.Context, c *client.Client, accountID string, kind accountKind, diags *diag.Diagnostics) {
	ctx, cancel := context.WithTimeout(ctx, accountCheckTimeout)
	defer cancel()
	res, err := c.DoRequestContext(ctx, `query { accounts { _id account_type } }`, nil)
	if err != nil {
		diags.AddError("Unable to read the accounts", fmt.Sprintf("Checking that account %s is %s: %s", accountID, kind.noun, err))
		return
	}
	accounts, _ := res["accounts"].([]interface{})
	for _, item := range accounts {
		account, _ := item.(map[string]interface{})
		if id, _ := account["_id"].(string); id != accountID {
			continue
		}
		if accountType, _ := account["account_type"].(string); accountType != kind.accountType {
			diags.AddAttributeError(path.Root("account_id"), "Not "+kind.noun,
				fmt.Sprintf("Account %s is a %s account: %s takes %s.", accountID, accountType, kind.resourceType, kind.noun))
		}
		return
	}
	diags.AddAttributeError(path.Root("account_id"), "Account not found",
		fmt.Sprintf("Account %s is not in Stream Security, or the token can't see it: account_id takes the Stream Security "+
			"account id (%s), not %s.", accountID, kind.ref, kind.nativeID))
}

func (r *ScannerHubResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ScannerHubResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hubID := data.AccountID.ValueString()
	coverage, accounts, found := readScannerHub(ctx, r.client, r.cloud.account.accountType, hubID, r.cloud.hints, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		tflog.Info(ctx, "Scanner hub no longer covers any account, removing it from state", map[string]interface{}{"account_id": hubID})
		resp.State.RemoveResource(ctx)
		return
	}

	data.Coverage = types.StringValue(coverage)
	if coverage == "selected" {
		ids := []attr.Value{}
		for _, account := range accounts {
			ids = append(ids, types.StringValue(account))
		}
		data.AccountIDs = types.SetValueMust(types.StringType, ids)
	} else if len(data.AccountIDs.Elements()) > 0 {
		// An "all" hub has no account_ids; the accounts pinned to it are not them.
		data.AccountIDs = types.SetNull(types.StringType)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// readScannerHub returns a hub's coverage and the accounts assigned to it, from GET
// scanner-coverage for its cloud; found is false when it is no longer a hub.
func readScannerHub(ctx context.Context, c *client.Client, cloud, hubID string, hints statusHints, diags *diag.Diagnostics) (coverage string, accounts []string, found bool) {
	var all scannerCoverage
	err := c.DoRESTRequest(ctx, http.MethodGet, client.AccountsPath("scanner-coverage")+"?cloud="+cloud, nil, &all)
	if err != nil {
		addAPIError(diags, "Unable to read the scanner coverage", err, hints)
		return "", nil, false
	}
	for _, hub := range all.Hubs {
		if hub.HubAccountID == hubID {
			coverage, found = hub.Coverage, true
			break
		}
	}
	if !found {
		return "", nil, false
	}
	for _, assignment := range all.Assignments {
		if assignment.HubAccountID == hubID {
			accounts = append(accounts, assignment.AccountID)
		}
	}
	return coverage, accounts, true
}

func (r *ScannerHubResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ScannerHubResourceModel
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

func (r *ScannerHubResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ScannerHubResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	turnOffScannerHub(ctx, r.client, data.AccountID.ValueString(), r.cloud.hints, &resp.Diagnostics)
}

func turnOffScannerHub(ctx context.Context, c *client.Client, hubID string, hints statusHints, diags *diag.Diagnostics) {
	err := c.DoRESTRequest(ctx, http.MethodPut, client.AccountsPath(hubID, "scanner-hub"),
		map[string]string{"coverage": "none"}, nil)
	if err != nil {
		addDeleteError(diags, "Unable to turn off the scanner hub", "Scanner hub already gone", err, hints, goneStatuses)
	}
}

func (r *ScannerHubResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("account_id"), req, resp)
}

func (r *ScannerHubResource) setCoverage(ctx context.Context, data ScannerHubResourceModel, diags *diag.Diagnostics) {
	accounts := []string{}
	if !data.AccountIDs.IsNull() && !data.AccountIDs.IsUnknown() {
		diags.Append(data.AccountIDs.ElementsAs(ctx, &accounts, false)...)
		if diags.HasError() {
			return
		}
	}
	putScannerHub(ctx, r.client, data.AccountID.ValueString(), data.Coverage.ValueString(), accounts, r.cloud.hints, diags)
}

func putScannerHub(ctx context.Context, c *client.Client, hubID, coverage string, accounts []string, hints statusHints, diags *diag.Diagnostics) {
	sort.Strings(accounts)
	err := c.DoRESTRequest(ctx, http.MethodPut, client.AccountsPath(hubID, "scanner-hub"),
		scannerHubRequest{Coverage: coverage, Accounts: accounts}, nil)
	if err != nil {
		addAPIError(diags, "Unable to set the scanner hub's coverage", err, hints)
	}
}

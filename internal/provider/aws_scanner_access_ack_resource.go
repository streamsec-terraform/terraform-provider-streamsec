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

var _ resource.Resource = &AWSScannerAccessAckResource{}
var _ resource.ResourceWithConfigure = &AWSScannerAccessAckResource{}

func NewAWSScannerAccessAckResource() resource.Resource {
	return &AWSScannerAccessAckResource{}
}

// AWSScannerAccessAckResource tells Stream Security that an AWS account now has
// the role a hub's scanner assumes to scan it, as the CloudFormation access
// stack's acknowledger does, without Terraform holding the hub's acknowledge token.
type AWSScannerAccessAckResource struct {
	client *client.Client
}

type AWSScannerAccessAckResourceModel struct {
	HubAccountID        types.String `tfsdk:"hub_account_id"`
	SpokeCloudAccountID types.String `tfsdk:"spoke_cloud_account_id"`
}

type scannerAccessAckRequest struct {
	SpokeCloudAccountID string `json:"spoke_cloud_account_id"`
	Status              string `json:"status"`
}

func scannerAccessAckHints(hubAccountID string) statusHints {
	return statusHints{
		http.StatusBadRequest: "Stream Security refused spoke_cloud_account_id: it must be a 12-digit AWS account id.",
		http.StatusForbidden:  fmt.Sprintf("The token has no access to account %s%s.", hubAccountID, trialExpiredHint),
		// Also the answer for an id that is no Stream Security account: there is no 404 here.
		http.StatusConflict: fmt.Sprintf("Account %s's scanner does not scan other accounts: make it a hub with streamsec_aws_scanner_hub "+
			"first. hub_account_id takes the hub's Stream Security account id (streamsec_aws_scanner_hub.<name>.account_id), "+
			"not its 12-digit AWS account id.", hubAccountID),
	}
}

func (r *AWSScannerAccessAckResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_scanner_access_ack"
}

func (r *AWSScannerAccessAckResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Acknowledges that an AWS account (a spoke) has the `StreamScannerSpokeRole` a hub's scanner assumes to scan it, " +
			"as the CloudFormation scanner access stack does. Destroying it reports the role uninstalled. " +
			"Used by the `volume-scanner-access` module of `terraform-streamsec-aws-account`.",

		Attributes: map[string]schema.Attribute{
			"hub_account_id": schema.StringAttribute{
				MarkdownDescription: "The Stream Security id of the hub's AWS account (`streamsec_aws_scanner_hub.<name>.account_id`). Changing it replaces the resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"spoke_cloud_account_id": schema.StringAttribute{
				MarkdownDescription: "The 12-digit AWS account id of the spoke the role is in. Changing it replaces the resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^\d{12}$`), "must be a 12-digit AWS account id"),
				},
			},
		},
	}
}

func (r *AWSScannerAccessAckResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

func (r *AWSScannerAccessAckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AWSScannerAccessAckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.acknowledge(ctx, data, "deployed"); err != nil {
		addAPIError(&resp.Diagnostics, "Unable to acknowledge the scanner access role", err, scannerAccessAckHints(data.HubAccountID.ValueString()))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read keeps the state: the acknowledgement has nothing to read back.
func (r *AWSScannerAccessAckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

// Update is never planned (every attribute replaces the resource); it acknowledges again.
func (r *AWSScannerAccessAckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AWSScannerAccessAckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.acknowledge(ctx, data, "deployed"); err != nil {
		addAPIError(&resp.Diagnostics, "Unable to acknowledge the scanner access role", err, scannerAccessAckHints(data.HubAccountID.ValueString()))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSScannerAccessAckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AWSScannerAccessAckResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.acknowledge(ctx, data, "uninstalled"); err != nil {
		addDeleteError(&resp.Diagnostics, "Unable to acknowledge the scanner access role uninstall", "Scanner access uninstall not recorded",
			err, scannerAccessAckHints(data.HubAccountID.ValueString()), uninstallGoneStatuses)
	}
}

func (r *AWSScannerAccessAckResource) acknowledge(ctx context.Context, data AWSScannerAccessAckResourceModel, status string) error {
	return r.client.DoRESTRequest(ctx, http.MethodPut,
		client.AccountsPath(data.HubAccountID.ValueString(), "scanner-access-terraform-acknowledge"),
		scannerAccessAckRequest{SpokeCloudAccountID: data.SpokeCloudAccountID.ValueString(), Status: status}, nil)
}

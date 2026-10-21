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

var _ resource.Resource = &AWSVolumeScannerAckResource{}
var _ resource.ResourceWithConfigure = &AWSVolumeScannerAckResource{}

// The shape ms_accounts accepts (its _AWS_REGION_RE), not a list of regions.
var awsRegionPattern = regexp.MustCompile(`^[a-z]{2,4}(-[a-z]{1,16}){1,3}-[0-9]{1,2}$`)

func NewAWSVolumeScannerAckResource() resource.Resource {
	return &AWSVolumeScannerAckResource{}
}

// AWSVolumeScannerAckResource tells Stream Security that the Terraform
// volume-scanner module is installed in a region, as the CloudFormation scanner
// stack's acknowledger does: this is what lets the scanner be a hub.
type AWSVolumeScannerAckResource struct {
	client *client.Client
}

type AWSVolumeScannerAckResourceModel struct {
	AccountID     types.String `tfsdk:"account_id"`
	Region        types.String `tfsdk:"region"`
	ModuleVersion types.String `tfsdk:"module_version"`
}

type volumeScannerAckRequest struct {
	Region        string `json:"region"`
	Status        string `json:"status"`
	ModuleVersion string `json:"module_version"`
}

func volumeScannerAckHints(accountID string) statusHints {
	return statusHints{
		http.StatusForbidden: fmt.Sprintf("The token has no access to account %s.", accountID),
		http.StatusNotFound: fmt.Sprintf("Account %s is not in Stream Security: account_id takes the Stream Security account id "+
			"(streamsec_aws_account.<name>.id), not the 12-digit AWS account id.", accountID),
		http.StatusConflict: fmt.Sprintf("Account %s is not a connected AWS account in Stream Security.", accountID),
	}
}

func (r *AWSVolumeScannerAckResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_volume_scanner_ack"
}

func (r *AWSVolumeScannerAckResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Acknowledges a Terraform install of the agentless volume scanner in one region of an AWS account, " +
			"as the CloudFormation scanner stack does, so the scanner can be a hub (`streamsec_aws_scanner_hub`). " +
			"Destroying it reports the scanner uninstalled. Used by the `volume-scanner` module of `terraform-streamsec-aws-account`.",

		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				MarkdownDescription: "The Stream Security id of the AWS account (`streamsec_aws_account.<name>.id`). Changing it replaces the resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "The AWS region the scanner is installed in. Changing it replaces the resource.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.RegexMatches(awsRegionPattern, "must be an AWS region, such as us-east-1"),
				},
			},
			"module_version": schema.StringAttribute{
				MarkdownDescription: "The version of the Terraform module that installed the scanner.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 64),
				},
			},
		},
	}
}

func (r *AWSVolumeScannerAckResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = providerClient(req.ProviderData, &resp.Diagnostics)
}

func (r *AWSVolumeScannerAckResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data AWSVolumeScannerAckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.acknowledge(ctx, data, "deployed"); err != nil {
		addAPIError(&resp.Diagnostics, "Unable to acknowledge the volume scanner", err, volumeScannerAckHints(data.AccountID.ValueString()))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read keeps the state: the acknowledgement has nothing to read back.
func (r *AWSVolumeScannerAckResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *AWSVolumeScannerAckResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data AWSVolumeScannerAckResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.acknowledge(ctx, data, "deployed"); err != nil {
		addAPIError(&resp.Diagnostics, "Unable to acknowledge the volume scanner", err, volumeScannerAckHints(data.AccountID.ValueString()))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AWSVolumeScannerAckResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data AWSVolumeScannerAckResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.acknowledge(ctx, data, "uninstalled")
	if err == nil {
		return
	}
	hints := volumeScannerAckHints(data.AccountID.ValueString())
	if hasStatus(err, http.StatusNotFound, http.StatusConflict) {
		// Nothing left in Stream Security to mark uninstalled; don't block the destroy.
		resp.Diagnostics.AddWarning("Volume scanner uninstall not recorded",
			fmt.Sprintf("Stream Security could not record the scanner in %s as uninstalled; it is removed from the Terraform state only.\n\n%s",
				data.Region.ValueString(), hints.detail(err)))
		return
	}
	addAPIError(&resp.Diagnostics, "Unable to acknowledge the volume scanner uninstall", err, hints)
}

func (r *AWSVolumeScannerAckResource) acknowledge(ctx context.Context, data AWSVolumeScannerAckResourceModel, status string) error {
	return r.client.DoRESTRequest(ctx, http.MethodPut,
		client.AccountsPath(data.AccountID.ValueString(), "scanner-terraform-acknowledge"),
		volumeScannerAckRequest{Region: data.Region.ValueString(), Status: status, ModuleVersion: data.ModuleVersion.ValueString()}, nil)
}

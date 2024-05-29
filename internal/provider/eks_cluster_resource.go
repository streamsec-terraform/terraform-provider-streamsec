package provider

import (
	"context"
	"fmt"
	"terraform-provider-streamsec/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &EKSClusterResource{}
var _ resource.ResourceWithImportState = &EKSClusterResource{}

func NewEKSClusterResource() resource.Resource {
	return &EKSClusterResource{}
}

type EKSClusterResource struct {
	client *client.Client
}
type EKSClusterResourceModel struct {
	ID              types.String `tfsdk:"id"`
	ARN             types.String `tfsdk:"arn"`
	DisplayName     types.String `tfsdk:"display_name"`
	Status          types.String `tfsdk:"status"`
	CollectionToken types.String `tfsdk:"collection_token"`
	CreationDate    types.String `tfsdk:"creation_date"`
}

func (r *EKSClusterResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_eks_cluster"
}

func (r *EKSClusterResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "EKSCluster resource",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The ID of the EKS cluster.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"arn": schema.StringAttribute{
				Description: "The arn of the EKS cluster.",
				Required:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				Description: "The display name of the EKS cluster.",
				Required:    true,
			},
			"status": schema.StringAttribute{
				Description: "The EKS cluster status.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"collection_token": schema.StringAttribute{
				Description: "The collection_token.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"creation_date": schema.StringAttribute{
				Description: "The creation_date.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *EKSClusterResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *EKSClusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data EKSClusterResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	query := `
		mutation CreateKubernetes($display_name: String, $arn: String) {
			createKubernetes(kubernetes: {
				display_name: $display_name,
				eks_arn: $arn,
			  })
			{
				_id
				status
				collection_token
				creation_date
			}
	}`

	variables := map[string]interface{}{
		"display_name": data.DisplayName.ValueString(),
		"arn":          data.ARN.ValueString()}

	tflog.Debug(ctx, fmt.Sprintf("variables: %v", variables))

	res, err := r.client.DoRequest(query, variables)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create account, got error: %s", err))
		return
	}

	tflog.Debug(ctx, fmt.Sprintf("Response: %v", res))

	cluster := res["createKubernetes"].(map[string]interface{})
	data.ID = types.StringValue(cluster["_id"].(string))
	data.Status = types.StringValue(cluster["status"].(string))
	data.CollectionToken = types.StringValue(cluster["collection_token"].(string))
	data.CreationDate = types.StringValue(cluster["creation_date"].(string))

	// Write logs using the tflog package
	// Documentation: https://terraform.io/plugin/log
	tflog.Trace(ctx, "created a resource")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *EKSClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data EKSClusterResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	query := `
		query {
			kubernetes {
				_id
				display_name
				status
				collection_token
				creation_date
			}
		}`

	res, err := r.client.DoRequest(query, nil)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to get account, got error: %s", err))
		return
	}

	clusters := res["kubernetes"].([]interface{})
	clusterFound := false

	for _, item := range clusters {

		cluster := item.(map[string]interface{})
		if cluster["eks_arn"].(string) == data.ARN.ValueString() {
			data.ID = types.StringValue(cluster["_id"].(string))
			data.DisplayName = types.StringValue(cluster["display_name"].(string))
			data.Status = types.StringValue(cluster["status"].(string))
			data.CollectionToken = types.StringValue(cluster["collection_token"].(string))
			data.CreationDate = types.StringValue(cluster["creation_date"].(string))
			clusterFound = true
		}
	}

	if !clusterFound {
		resp.Diagnostics.AddError("Resource not found", fmt.Sprintf("Unable to get EKS cluster, cluster with ARN: %s not found in Stream.Security API.", data.ARN.ValueString()))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *EKSClusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data EKSClusterResourceModel
	var state EKSClusterResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// check if there was a change in display_name
	if data.DisplayName != state.DisplayName {
		query := `
			mutation UpdateKubernetes($id: ID!, $kubernetes: EditKubernetesInput) {
				updateKubernetes(id: $id, kubernetes: $kubernetes) {
					_id
				}
			}`

		variables := map[string]interface{}{
			"id": data.ID.ValueString(),
			"kubernetes": map[string]interface{}{
				"display_name": data.DisplayName.ValueString()}}

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

func (r *EKSClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data EKSClusterResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	query := `
		mutation DeleteKubernetes($id: ID!) {
			deleteKubernetes(id: $id)
		}`

	variables := map[string]interface{}{
		"id": data.ID.ValueString()}

	_, err := r.client.DoRequest(query, variables)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete account, got error: %s", err))
		return
	}
}

func (r *EKSClusterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("arn"), req, resp)
}

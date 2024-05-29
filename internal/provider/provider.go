// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"os"
	"terraform-provider-streamsec/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure StreamsecProvider satisfies various provider interfaces.
var _ provider.Provider = &StreamsecProvider{}
var _ provider.ProviderWithFunctions = &StreamsecProvider{}

// StreamsecProvider defines the provider implementation.
type StreamsecProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// StreamsecProviderModel describes the provider data model.
type StreamsecProviderModel struct {
	Host        types.String `tfsdk:"host"`
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
	WorkspaceId types.String `tfsdk:"workspace_id"`
}

func (p *StreamsecProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "streamsec"
	resp.Version = p.version
}

func (p *StreamsecProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Required: true,
			},
			"username": schema.StringAttribute{
				Required: true,
			},
			"password": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
			},
			"workspace_id": schema.StringAttribute{
				Required: true,
			},
		},
	}
}

func (p *StreamsecProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	// Retrieve provider data from configuration
	var config StreamsecProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	// If practitioner provided a configuration value for any of the
	// attributes, it must be a known value.
	if config.Host.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Unknown Stream.Security API Host",
			"The provider cannot create the Stream.Security API client as there is an unknown configuration value for the Stream.Security API host. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the STREAMSEC_HOST environment variable.",
		)
	}
	if config.Username.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("username"),
			"Unknown Stream.Security API Username",
			"The provider cannot create the Stream.Security API client as there is an unknown configuration value for the Stream.Security API username. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the STREAMSEC_USERNAME environment variable.",
		)
	}
	if config.Password.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("password"),
			"Unknown Stream.Security API Password",
			"The provider cannot create the Stream.Security API client as there is an unknown configuration value for the Stream.Security API password. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the STREAMSEC_PASSWORD environment variable.",
		)
	}
	if config.WorkspaceId.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("workspace_id"),
			"Unknown Stream.Security API Workspace ID",
			"The provider cannot create the Stream.Security API client as there is an unknown configuration value for the Stream.Security API workspace ID. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the STREAMSEC_WORKSPACE_ID environment variable.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	// Default values to environment variables, but override
	// with Terraform configuration value if set.
	host := os.Getenv("STREAMSEC_HOST")
	username := os.Getenv("STREAMSEC_USERNAME")
	password := os.Getenv("STREAMSEC_PASSWORD")
	workspaceId := os.Getenv("STREAMSEC_WORKSPACE_ID")
	if !config.Host.IsNull() {
		host = config.Host.ValueString()
	}
	if !config.Username.IsNull() {
		username = config.Username.ValueString()
	}
	if !config.Password.IsNull() {
		password = config.Password.ValueString()
	}
	if !config.WorkspaceId.IsNull() {
		workspaceId = config.WorkspaceId.ValueString()
	}

	// If any of the expected configurations are missing, return
	// errors with provider-specific guidance.
	if host == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing Stream.Security API Host",
			"The provider cannot create the Stream.Security API client as there is a missing or empty value for the Stream.Security API host. "+
				"Set the host value in the configuration or use the STREAMSEC_HOST environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}
	if username == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("username"),
			"Missing Stream.Security API Username",
			"The provider cannot create the Stream.Security API client as there is a missing or empty value for the Stream.Security API username. "+
				"Set the username value in the configuration or use the STREAMSEC_USERNAME environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}
	if password == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("password"),
			"Missing Stream.Security API Password",
			"The provider cannot create the Stream.Security API client as there is a missing or empty value for the Stream.Security API password. "+
				"Set the password value in the configuration or use the STREAMSEC_PASSWORD environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}
	if workspaceId == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("workspace_id"),
			"Missing Stream.Security API Workspace ID",
			"The provider cannot create the Stream.Security API client as there is a missing or empty value for the Stream.Security API workspace ID. "+
				"Set the workspace_id value in the configuration or use the STREAMSEC_WORKSPACE_ID environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	// Create a new Stream.Security client using the configuration values
	client, err := client.NewClient(&host, &username, &password, &workspaceId)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Stream.Security API Client",
			"An unexpected error occurred when creating the Stream.Security API client. "+
				"If the error is not clear, please contact the provider developers.\n\n"+
				"Stream.Security Client Error: "+err.Error(),
		)
		return
	}
	// Make the Stream.Security client available during DataSource and Resource
	// type Configure methods.
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *StreamsecProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAWSAccountResource,
		NewEKSClusterResource,
		NewAWSAccountAckResource,
	}
}

func (p *StreamsecProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func (p *StreamsecProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &StreamsecProvider{
			version: version,
		}
	}
}

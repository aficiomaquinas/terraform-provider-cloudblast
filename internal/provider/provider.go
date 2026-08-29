
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &CloudBlastProvider{}

type CloudBlastProvider struct {
	version string
}

type CloudBlastProviderModel struct {
	APIToken types.String `tfsdk:"api_token"`
	Endpoint types.String `tfsdk:"endpoint"`
}

func (p *CloudBlastProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cloudblast"
	resp.Version = p.version
}

func (p *CloudBlastProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The CloudBlast provider manages cloud infrastructure via the CloudBlast REST API v2.",
		Attributes: map[string]schema.Attribute{
			"api_token": schema.StringAttribute{
				MarkdownDescription: "API token for authentication. Can also be set via the `CLOUDBLAST_API_TOKEN` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "API base URL. Defaults to `https://console.cloudblast.io/api/v2`. Can also be set via the `CLOUDBLAST_API_URL` environment variable.",
				Optional:            true,
			},
		},
	}
}

func (p *CloudBlastProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data CloudBlastProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve API token: config > env
	apiToken := data.APIToken.ValueString()
	if apiToken == "" {
		apiToken = os.Getenv("CLOUDBLAST_API_TOKEN")
	}
	if apiToken == "" {
		resp.Diagnostics.AddError("Missing API token", "Set api_token in the provider config or CLOUDBLAST_API_TOKEN environment variable.")
		return
	}

	// Resolve endpoint: config > env > default
	endpoint := data.Endpoint.ValueString()
	if endpoint == "" {
		endpoint = os.Getenv("CLOUDBLAST_API_URL")
	}

	client := NewClient(endpoint, apiToken)
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *CloudBlastProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewServerResource,
		NewSSHKeyResource,
		NewSecurityGroupResource,
		NewFirewallRuleResource,
	}
}

func (p *CloudBlastProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewPlansDataSource,
		NewLocationsDataSource,
		NewTemplatesDataSource,
		NewAccountDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &CloudBlastProvider{
			version: version,
		}
	}
}

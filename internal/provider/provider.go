package provider

import (
	"context"
	"os"
	"strings"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	tb "terrascale.tech/sdk/v1"
)

type terraScaleProvider struct{ version string }
type providerModel struct {
	AccessToken  types.String `tfsdk:"access_token"`
	ProjectToken types.String `tfsdk:"project_token"`
}
type clients struct {
	management *tb.ManagementUser
	data       *tb.Client
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &terraScaleProvider{version: version} }
}
func (p *terraScaleProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "terrascale"
	resp.Version = p.version
}
func (p *terraScaleProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manage TerraScale projects, databases, collections and project API keys through the public SDK.", Attributes: map[string]schema.Attribute{
		"access_token":  schema.StringAttribute{Optional: true, Sensitive: true, Description: "ts-auth user access token for management resources. Defaults to TERRASCALE_ACCESS_TOKEN. Never use an internal peer credential."},
		"project_token": schema.StringAttribute{Optional: true, Sensitive: true, Description: "Existing project API key or project access token for collections. Defaults to TERRASCALE_PROJECT_TOKEN. Must authorize the resource's project."},
	}}
}
func (p *terraScaleProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c := &clients{}
	if !config.AccessToken.IsUnknown() {
		token := os.Getenv("TERRASCALE_ACCESS_TOKEN")
		if !config.AccessToken.IsNull() {
			token = config.AccessToken.ValueString()
		}
		if token != "" {
			if strings.ContainsFunc(token, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
				resp.Diagnostics.AddError("Invalid access token", "Use a ts-auth user access token without whitespace or control characters.")
				return
			}
			m, err := tb.NewManagement(tb.Config{})
			if err != nil {
				resp.Diagnostics.AddError("Client configuration failed", "Could not initialize management client.")
				return
			}
			c.management = m.WithToken(token)
		}
	}
	if !config.ProjectToken.IsUnknown() {
		token := os.Getenv("TERRASCALE_PROJECT_TOKEN")
		if !config.ProjectToken.IsNull() {
			token = config.ProjectToken.ValueString()
		}
		if token != "" {
			data, err := tb.New(tb.Config{Token: token})
			if err != nil {
				resp.Diagnostics.AddError("Invalid project token", "Use an existing tb_ project API key or ts-auth project access token.")
				return
			}
			c.data = data
		}
	}
	resp.ResourceData = c
}
func (p *terraScaleProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{func() resource.Resource { return &projectResource{} }, func() resource.Resource { return &databaseResource{} }, func() resource.Resource { return &collectionResource{} }, func() resource.Resource { return &apiKeyResource{} }}
}
func (p *terraScaleProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }

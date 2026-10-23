package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"terrascale.tech/sdk/tsauth"
)

type apiKeyResource struct{ clients *clients }
type apiKeyModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	Name           types.String `tfsdk:"name"`
	DatabaseName   types.String `tfsdk:"database_name"`
	Scopes         types.Set    `tfsdk:"scopes"`
	Secret         types.String `tfsdk:"secret"`
}

func (r *apiKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}
func (r *apiKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Project API key. The secret is stored in Terraform state: sensitive only hides display. Use an encrypted restricted state backend. Imports cannot recover secrets. Every configuration change replaces and revokes the old key.", Attributes: map[string]schema.Attribute{
		"id": stateID(), "organization_id": idAttribute("Owning organization ULID."), "project_id": idAttribute("Owning project ULID."),
		"name":          schema.StringAttribute{Required: true, Description: "Key display name; changes replace the key.", Validators: []validator.String{displayNameValidator{}}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"database_name": schema.StringAttribute{Optional: true, Description: "Restrict to a database name. Omit to reach every database of the project. Changes replace the key.", Validators: []validator.String{stringvalidator.RegexMatches(namePattern, "Use a lowercase database name, up to 63 characters.")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"scopes":        schema.SetAttribute{Required: true, ElementType: types.StringType, Description: "Nonempty subset of documents:read and documents:write. Changes replace the key.", Validators: []validator.Set{setvalidator.SizeAtLeast(1), setvalidator.NoNullValues(), setvalidator.ValueStringsAre(stringvalidator.OneOf("documents:read", "documents:write"))}, PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()}},
		"secret":        schema.StringAttribute{Computed: true, Sensitive: true, Description: "Secret returned once on creation, persisted in state. Null for imported keys."},
	}}
}
func (r *apiKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configure(req, resp, &r.clients)
}
func (m *apiKeyModel) apply(ctx context.Context, p tsauth.APIKey) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	if p.Database == "" {
		m.DatabaseName = types.StringNull()
	} else {
		m.DatabaseName = types.StringValue(p.Database)
	}
	m.Scopes, _ = types.SetValueFrom(ctx, types.StringType, p.Scopes)
}
func (r *apiKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m apiKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	var scopes []string
	resp.Diagnostics.Append(m.Scopes.ElementsAs(ctx, &scopes, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.clients.management.CreateAPIKey(ctx, m.OrganizationID.ValueString(), m.ProjectID.ValueString(), tsauth.APIKeyRequest{Name: m.Name.ValueString(), Database: m.DatabaseName.ValueString(), Scopes: scopes})
	if err != nil {
		apiError(&resp.Diagnostics, "Create API key", err)
		return
	}
	m.apply(ctx, p.APIKey)
	m.Secret = types.StringValue(p.Key)
	if p.Key == "" {
		m.Secret = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if p.Key == "" {
		resp.Diagnostics.AddError("Created key has no secret", "The API created a key but returned no secret. Its ID is retained in state. Revoke or replace it deliberately; the existing secret cannot be recovered.")
		return
	}
	resp.Diagnostics.AddWarning("API key secret stored in state", "Sensitive hides display only. Protect state and plan files with encryption and restricted access; the server never returns this secret again.")
}
func (r *apiKeyResource) find(ctx context.Context, m apiKeyModel) (*tsauth.APIKey, error) {
	seen := map[string]bool{}
	after := ""
	for {
		page, err := r.clients.management.APIKeys(ctx, m.OrganizationID.ValueString(), m.ProjectID.ValueString(), tsauth.PageOptions{Limit: 100, After: after})
		if err != nil {
			return nil, err
		}
		for _, key := range page.Items {
			if key.ID == m.ID.ValueString() {
				return &key, nil
			}
		}
		if page.Next == "" {
			return nil, nil
		}
		if seen[page.Next] {
			return nil, errors.New("repeated API key pagination cursor")
		}
		seen[page.Next] = true
		after = page.Next
	}
}
func (r *apiKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m apiKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	p, err := r.find(ctx, m)
	if notFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Read API key", err)
		return
	}
	if p == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	m.apply(ctx, *p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *apiKeyResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("API key update unavailable", "API key changes require replacement.")
}
func (r *apiKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m apiKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	err := r.clients.management.RevokeAPIKey(ctx, m.OrganizationID.ValueString(), m.ProjectID.ValueString(), m.ID.ValueString())
	if err != nil && !notFound(err) {
		apiError(&resp.Diagnostics, "Revoke API key", err)
	}
}
func (r *apiKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setImport(ctx, req.ID, resp, "organization_id", "project_id", "resource_id")
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("secret"), types.StringNull())...)
	resp.Diagnostics.AddWarning("Imported secret unavailable", "The server never returns an existing key's secret. secret remains null; import does not rotate the key.")
}

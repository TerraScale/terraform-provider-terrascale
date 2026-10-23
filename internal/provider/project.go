package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectResource struct{ clients *clients }
type projectModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Name           types.String `tfsdk:"name"`
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}
func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "A project within an existing organization. Deletion requires an empty project.", Attributes: map[string]schema.Attribute{
		"id": stateID(), "organization_id": idAttribute("Owning organization ULID; changing it replaces the project."),
		"name": schema.StringAttribute{Required: true, Description: "Project display name. Can be renamed in place.", Validators: []validator.String{displayNameValidator{}}},
	}}
}
func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configure(req, resp, &r.clients)
}
func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	p, err := r.clients.management.CreateProject(ctx, m.OrganizationID.ValueString(), m.Name.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Create project", err)
		return
	}
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	p, err := r.clients.management.Project(ctx, m.OrganizationID.ValueString(), m.ID.ValueString())
	if notFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Read project", err)
		return
	}
	m.Name = types.StringValue(p.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	p, err := r.clients.management.RenameProject(ctx, m.OrganizationID.ValueString(), m.ID.ValueString(), m.Name.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Rename project", err)
		return
	}
	m.Name = types.StringValue(p.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	err := r.clients.management.DeleteProject(ctx, m.OrganizationID.ValueString(), m.ID.ValueString())
	if err != nil && !notFound(err) {
		apiError(&resp.Diagnostics, "Delete project", err)
	}
}
func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setImport(ctx, req.ID, resp, "organization_id", "resource_id")
}

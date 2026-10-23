package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type collectionResource struct{ clients *clients }
type collectionModel struct {
	ID           types.String `tfsdk:"id"`
	ProjectID    types.String `tfsdk:"project_id"`
	DatabaseName types.String `tfsdk:"database_name"`
	Name         types.String `tfsdk:"name"`
}

func (r *collectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection"
}
func (r *collectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	name := schema.StringAttribute{Required: true, Description: "Lowercase name, up to 63 characters. Changes replace the collection and delete its documents.", Validators: []validator.String{stringvalidator.RegexMatches(namePattern, "Use a lowercase name starting with a letter, followed by letters, digits, _ or -; maximum 63 characters.")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}
	resp.Schema = schema.Schema{Description: "An empty schemaless collection, accessed by project credential. Deletion removes every document. Use lifecycle.prevent_destroy to protect data.", Attributes: map[string]schema.Attribute{
		"id": stateID(), "project_id": idAttribute("Owning project ULID."), "database_name": name, "name": name,
	}}
}
func (r *collectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configure(req, resp, &r.clients)
}
func (r *collectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m collectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireData(r.clients, &resp.Diagnostics) {
		return
	}
	p, err := r.clients.data.Database(m.ProjectID.ValueString(), m.DatabaseName.ValueString()).CreateCollection(ctx, m.Name.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Create collection", err)
		return
	}
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *collectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m collectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireData(r.clients, &resp.Diagnostics) {
		return
	}
	p, err := r.clients.data.Database(m.ProjectID.ValueString(), m.DatabaseName.ValueString()).Collection(m.Name.ValueString()).Info(ctx)
	if notFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Read collection", err)
		return
	}
	// Name-based routes must not adopt a different collection recreated at the same name.
	if !m.ID.IsNull() && m.ID.ValueString() != p.ID {
		resp.State.RemoveResource(ctx)
		return
	}
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *collectionResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Collection update unavailable", "Collection changes require replacement.")
}
func (r *collectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m collectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireData(r.clients, &resp.Diagnostics) {
		return
	}
	col := r.clients.data.Database(m.ProjectID.ValueString(), m.DatabaseName.ValueString()).Collection(m.Name.ValueString())
	current, err := col.Info(ctx)
	if notFound(err) {
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Read collection before deletion", err)
		return
	}
	if current.ID != m.ID.ValueString() {
		resp.Diagnostics.AddError("Collection identity changed", "The collection at this name has a different ID. Refresh state before deletion.")
		return
	}
	// shortcut: identity check and name-based drop are separate; use conditional drop when the SDK supports it.
	_, err = col.Drop(ctx)
	if err != nil && !notFound(err) {
		apiError(&resp.Diagnostics, "Delete collection", err)
	}
}
func (r *collectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setImport(ctx, req.ID, resp, "project_id", "database_name", "name")
}

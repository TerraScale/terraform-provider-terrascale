package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	tb "terrascale.tech/sdk/v1"
)

type databaseResource struct{ clients *clients }
type databaseModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	Name           types.String `tfsdk:"name"`
	Regions        types.Set    `tfsdk:"regions"`
	AllRegions     types.Bool   `tfsdk:"all_regions"`
}

func (r *databaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}
func (r *databaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "A database and its serving regions. Name or parent changes replace it and delete its data.", Attributes: map[string]schema.Attribute{
		"id": stateID(), "organization_id": idAttribute("Owning organization ULID."), "project_id": idAttribute("Owning project ULID."),
		"name":        schema.StringAttribute{Required: true, Description: "Lowercase database name, up to 63 characters. Changes replace the database.", Validators: []validator.String{stringvalidator.RegexMatches(namePattern, "Use a lowercase name starting with a letter, followed by letters, digits, _ or -; maximum 63 characters.")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"regions":     schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, Description: "Complete set of one to nine serving region IDs. Omit both placement controls to accept server placement or preserve existing placement. Explicit regions select listed placement when all_regions is omitted.", Validators: []validator.Set{setvalidator.SizeBetween(1, 9), setvalidator.NoNullValues(), setvalidator.ValueStringsAre(stringvalidator.RegexMatches(namePattern, "Use a region ID such as gru."))}},
		"all_regions": schema.BoolAttribute{Optional: true, Computed: true, Description: "Serve from every region, including future regions. Cannot be combined with explicit regions. Omit both controls to preserve placement."},
	}}
}
func (r *databaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configure(req, resp, &r.clients)
}
func (r *databaseResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || !req.Plan.Raw.IsKnown() {
		return
	}
	var config databaseModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.AllRegions.IsNull() && !config.Regions.IsNull() && !config.Regions.IsUnknown() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("all_regions"), types.BoolValue(false))...)
	}
}
func (r *databaseResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m databaseModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if m.AllRegions.ValueBool() && !m.Regions.IsNull() {
		resp.Diagnostics.AddError("Conflicting placement", "Set regions or all_regions=true, not both.")
	}
}
func (m *databaseModel) apply(ctx context.Context, p *tb.ManagedDatabase) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	m.Regions, _ = types.SetValueFrom(ctx, types.StringType, append([]string{}, p.Regions...))
	m.AllRegions = types.BoolValue(p.AllRegions)
}
func (r *databaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	client, err := r.clients.project(m.OrganizationID.ValueString(), m.ProjectID.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Initialize database client", err)
		return
	}
	var regions []string
	if !m.Regions.IsNull() && !m.Regions.IsUnknown() && !m.AllRegions.ValueBool() {
		resp.Diagnostics.Append(m.Regions.ElementsAs(ctx, &regions, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := client.CreateDatabase(ctx, m.ProjectID.ValueString(), m.Name.ValueString(), tb.CreateDatabaseOptions{Regions: regions, AllRegions: m.AllRegions.ValueBool()})
	if err != nil {
		apiError(&resp.Diagnostics, "Create database", err)
		return
	}
	m.apply(ctx, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *databaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	db, err := m.database(ctx, r.clients)
	var p *tb.ManagedDatabase
	if err == nil {
		p, err = db.Info(ctx)
	}
	if notFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Read database", err)
		return
	}
	if !m.ID.IsNull() && m.ID.ValueString() != p.ID {
		resp.State.RemoveResource(ctx)
		return
	}
	m.apply(ctx, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *databaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m, prior databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	client, err := r.clients.project(m.OrganizationID.ValueString(), m.ProjectID.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "Initialize database client", err)
		return
	}
	placement := tb.DatabasePlacement{AllRegions: m.AllRegions.ValueBool()}
	if !placement.AllRegions {
		regions := m.Regions
		if regions.IsNull() || regions.IsUnknown() {
			regions = prior.Regions
		}
		resp.Diagnostics.Append(regions.ElementsAs(ctx, &placement.Regions, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := client.SetDatabasePlacement(ctx, m.ProjectID.ValueString(), m.ID.ValueString(), placement)
	if err != nil {
		apiError(&resp.Diagnostics, "Update database regions", err)
		return
	}
	m.apply(ctx, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *databaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	db, err := m.database(ctx, r.clients)
	if err == nil {
		err = db.Delete(ctx)
	}
	if err != nil && !notFound(err) {
		apiError(&resp.Diagnostics, "Delete database", err)
	}
}
func (r *databaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setImport(ctx, req.ID, resp, "organization_id", "project_id", "resource_id")
}

func (m *databaseModel) database(_ context.Context, c *clients) (*tb.Database, error) {
	client, err := c.project(m.OrganizationID.ValueString(), m.ProjectID.ValueString())
	if err != nil {
		return nil, err
	}
	return client.Database(m.ProjectID.ValueString(), m.ID.ValueString()), nil
}

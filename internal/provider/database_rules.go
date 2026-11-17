package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	tb "terrascale.tech/sdk/v1"
)

type databaseRulesResource struct{ clients *clients }
type databaseRulesModel struct {
	Project  types.String `tfsdk:"project"`
	Database types.String `tfsdk:"database"`
	Rules    rulesValue   `tfsdk:"rules"`
	Version  types.String `tfsdk:"version"`
}

func (r *databaseRulesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database_rules"
}
func (r *databaseRulesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "The complete end-user rules for a database. Deletion deploys empty rules, denying every end-user operation. Requires a project credential with databases:read and databases:write.", Attributes: map[string]schema.Attribute{
		"project":  idAttribute("Owning project ULID. Changes replace the rules resource."),
		"database": schema.StringAttribute{Required: true, Description: "Database name. Changes replace the rules resource.", Validators: []validator.String{stringvalidator.RegexMatches(namePattern, "Use a lowercase database name, up to 63 characters.")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"rules":    schema.StringAttribute{Required: true, CustomType: rulesType{}, Description: "Complete rules JSON with a tables object. Key order and whitespace do not cause changes. Missing operations are denied.", Validators: []validator.String{rulesValidator{}}},
		"version":  schema.StringAttribute{Computed: true, Description: "Current rules version ULID. Refresh reads the live version; deployments use the state version as If-Match."},
	}}
}
func (r *databaseRulesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configure(req, resp, &r.clients)
}
func (r *databaseRulesResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || req.State.Raw.IsNull() {
		return
	}
	var plan, prior databaseRulesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() || plan.Rules.IsUnknown() || !plan.Project.Equal(prior.Project) || !plan.Database.Equal(prior.Database) {
		return
	}
	equal, diagnostics := prior.Rules.StringSemanticEquals(ctx, plan.Rules)
	resp.Diagnostics.Append(diagnostics...)
	if equal {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("rules"), prior.Rules)...)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("version"), prior.Version)...)
	}
}
func (r *databaseRulesResource) database(m databaseRulesModel, d *diag.Diagnostics) *tb.Database {
	if r.clients == nil || r.clients.data == nil {
		d.AddError("Missing project token", "Set project_token or TERRASCALE_PROJECT_TOKEN to a credential for this project with databases:read and databases:write.")
		return nil
	}
	return r.clients.data.Database(m.Project.ValueString(), m.Database.ValueString())
}
func (r *databaseRulesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m databaseRulesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.deploy(ctx, &m, nil, &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	}
}
func (r *databaseRulesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m databaseRulesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	db := r.database(m, &resp.Diagnostics)
	if db == nil {
		return
	}
	current, err := db.GetRules(ctx)
	if rulesMissing(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		rulesError(&resp.Diagnostics, "Read database rules", err)
		return
	}
	body, err := json.Marshal(tb.RulesSet{Tables: current.Tables})
	if err != nil {
		resp.Diagnostics.AddError("Read database rules failed", "Could not encode the current rules JSON.")
		return
	}
	m.Rules = rulesValue{types.StringValue(string(body))}
	m.Version = types.StringValue(current.Version)
	rulesProblems(&resp.Diagnostics, current.Problems, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *databaseRulesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m, prior databaseRulesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if prior.Version.IsNull() || prior.Version.IsUnknown() {
		resp.Diagnostics.AddError("Missing rules version", "Run terraform refresh before updating database rules.")
		return
	}
	r.deploy(ctx, &m, prior.Version.ValueStringPointer(), &resp.Diagnostics)
	if !resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	}
}
func (r *databaseRulesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m databaseRulesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	db := r.database(m, &resp.Diagnostics)
	if db == nil {
		return
	}
	if m.Version.IsNull() || m.Version.IsUnknown() {
		resp.Diagnostics.AddError("Missing rules version", "Run terraform refresh before deleting database rules.")
		return
	}
	_, err := db.DeployRules(ctx, tb.RulesSet{Tables: map[string]tb.RulesTable{}}, m.Version.ValueStringPointer())
	if err != nil && !rulesMissing(err) {
		rulesError(&resp.Diagnostics, "Reset database rules", err)
	}
}
func (r *databaseRulesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := importParts(req.ID, "project_id", "database")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import identifier", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), parts[1])...)
}
func (r *databaseRulesResource) deploy(ctx context.Context, m *databaseRulesModel, version *string, d *diag.Diagnostics) {
	db := r.database(*m, d)
	if db == nil {
		return
	}
	rules, err := decodeRules(m.Rules.ValueString())
	if err != nil {
		d.AddAttributeError(path.Root("rules"), "Invalid rules JSON", err.Error())
		return
	}
	current, err := db.DeployRules(ctx, rules, version)
	if err != nil {
		rulesError(d, "Deploy database rules", err)
		return
	}
	m.Version = types.StringValue(current.Version)
	rulesProblems(d, current.Warnings, false)
}
func rulesMissing(err error) bool {
	var api *tb.APIError
	return errors.As(err, &api) && api.Status == http.StatusNotFound && (api.Code == "no_rules" || api.Code == tb.ErrUnknownDatabase)
}
func rulesError(d *diag.Diagnostics, operation string, err error) {
	var api *tb.APIError
	if errors.As(err, &api) {
		if api.Status == http.StatusPreconditionFailed {
			d.AddError(operation+" failed", "rules changed outside Terraform; run terraform refresh")
			return
		}
		if api.Code == tb.ErrInvalidRules && len(api.Problems) > 0 {
			rulesProblems(d, api.Problems, true)
			return
		}
	}
	apiError(d, operation, err)
}
func rulesProblems(d *diag.Diagnostics, problems []tb.RulesProblem, failure bool) {
	for _, problem := range problems {
		detail := fmt.Sprintf("Table %q, operation/field %q: %s (%s)", problem.Table, problem.Field, problem.Message, problem.Code)
		if failure {
			d.AddAttributeError(path.Root("rules"), "Invalid database rules", detail)
		} else {
			d.AddAttributeWarning(path.Root("rules"), "Database rules problem", detail)
		}
	}
}

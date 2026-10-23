package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	tb "terrascale.tech/sdk/v1"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

type idValidator struct{}

func (idValidator) Description(context.Context) string               { return "a canonical uppercase ULID" }
func (v idValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (idValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if tb.ValidateID(req.ConfigValue.ValueString()) != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid identifier", "Use a canonical uppercase ULID, without a prefix.")
	}
}

type displayNameValidator struct{}

func (displayNameValidator) Description(context.Context) string {
	return "1–128 characters, no surrounding whitespace or controls, including a letter or digit"
}
func (v displayNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}
func (v displayNameValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	name := req.ConfigValue.ValueString()
	length := 0
	for _, r := range name {
		length += utf16.RuneLen(r)
	}
	if length == 0 || length > 128 || strings.TrimSpace(name) != name || strings.ContainsFunc(name, unicode.IsControl) || !strings.ContainsFunc(name, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid name", v.Description(ctx))
	}
}

func idAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{Required: true, Description: description, Validators: []validator.String{idValidator{}}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}}
}
func stateID() schema.StringAttribute {
	return schema.StringAttribute{Computed: true, Description: "Canonical uppercase ULID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}
}
func configure(req resource.ConfigureRequest, resp *resource.ConfigureResponse, target **clients) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*clients)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider configuration", "Expected TerraScale SDK clients.")
		return
	}
	*target = c
}
func requireManagement(c *clients, d *diag.Diagnostics) bool {
	if c == nil || c.management == nil {
		d.AddError("Missing access token", "Set access_token or TERRASCALE_ACCESS_TOKEN to a ts-auth user access token.")
		return false
	}
	return true
}
func requireData(c *clients, d *diag.Diagnostics) bool {
	if c == nil || c.data == nil {
		d.AddError("Missing project token", "Set project_token or TERRASCALE_PROJECT_TOKEN to an existing credential for this collection's project.")
		return false
	}
	return true
}
func notFound(err error) bool {
	var api *tb.APIError
	return errors.As(err, &api) && api.Status == http.StatusNotFound
}
func apiError(d *diag.Diagnostics, operation string, err error) {
	// Server messages and transport errors can contain credentials or response bodies.
	detail := "Request failed. Check credentials, permissions and service availability. An interrupted mutation may have committed; inspect or import the resource before retrying."
	var api *tb.APIError
	if errors.As(err, &api) {
		detail = fmt.Sprintf("TerraScale returned HTTP %d. Check permissions and the requested resource. An uncertain mutation may require importing the resource before retrying.", api.Status)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		detail = "Request canceled or timed out. A mutation may have committed; inspect or import the resource before retrying."
	}
	d.AddError(operation+" failed", detail)
}
func importParts(id string, kinds ...string) ([]string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != len(kinds) {
		return nil, fmt.Errorf("expected %s", strings.Join(kinds, "/"))
	}
	for i, kind := range kinds {
		if strings.HasSuffix(kind, "_id") {
			if tb.ValidateID(parts[i]) != nil {
				return nil, fmt.Errorf("%s must be a canonical uppercase ULID", kind)
			}
		} else if !namePattern.MatchString(parts[i]) {
			return nil, fmt.Errorf("%s must match %s", kind, namePattern)
		}
	}
	return parts, nil
}
func setImport(ctx context.Context, id string, resp *resource.ImportStateResponse, fields ...string) {
	parts, err := importParts(id, fields...)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import identifier", err.Error())
		return
	}
	for i, field := range fields {
		if field == "resource_id" {
			field = "id"
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(field), parts[i])...)
	}
}

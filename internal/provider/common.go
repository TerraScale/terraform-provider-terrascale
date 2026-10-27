package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"terrascale.tech/sdk/tsauth"
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

// notFound reports a typed resource-absent 404. A 404 with another code,
// such as unknown_route from an API path the server no longer serves or an
// untyped proxy 404, is an error: removing state for it would make Terraform
// plan to recreate resources that still exist.
func notFound(err error) bool {
	var api *tb.APIError
	var auth *tsauth.APIError
	switch {
	case errors.As(err, &api):
		return api.Status == http.StatusNotFound && (api.Code == tb.ErrUnknownDatabase || api.Code == tb.ErrUnknownCollection || api.Code == tb.ErrNotFound)
	case errors.As(err, &auth):
		return auth.Status == http.StatusNotFound && auth.Code == tsauth.ErrNotFound
	}
	return false
}

var (
	errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// errorGuidance explains the typed codes a provider operation can meet.
var errorGuidance = map[string]string{
	"name_taken":                "A resource with this name already exists. Import it with terraform import, or choose another name.",
	"conflict":                  "The request conflicts with the current resource state.",
	"not_empty":                 "The resource still contains children; delete them first.",
	"forbidden_scope":           "The credential lacks a required scope for this operation.",
	"forbidden":                 "The user's role does not allow this operation.",
	"forbidden_role":            "The user's role does not allow this operation.",
	"unknown_database":          "The database does not exist, or project_token belongs to a different project.",
	"unknown_collection":        "The collection does not exist.",
	"not_found":                 "The resource does not exist, or the user cannot reach its organization.",
	"unknown_route":             "The API does not serve this route. Upgrade the provider to a release that matches the current TerraScale API.",
	"invalid_request":           "The API rejected the request as invalid. Check the configured values.",
	"quota_exceeded":            "The project quota is exhausted.",
	"rate_limited":              "TerraScale is rate limiting these requests. Wait, then retry.",
	"capacity":                  "TerraScale is temporarily over capacity. Wait, then retry.",
	"storage_unavailable":       "TerraScale storage is temporarily unavailable. Wait, then retry.",
	"dependencies_unavailable":  "A TerraScale dependency is temporarily unavailable. Wait, then retry.",
	"auth_unavailable":          "TerraScale authentication is temporarily unavailable. Wait, then retry.",
	"remote_region_unavailable": "A region holding this database is temporarily unavailable. Wait, then retry.",
	"temporarily_unavailable":   "TerraScale is temporarily unavailable. Wait, then retry.",
	"internal_error":            "TerraScale failed while handling the request; its effect is unknown.",
}

func apiError(d *diag.Diagnostics, operation string, err error) {
	// Server messages and transport errors can contain credentials or response
	// bodies; only validated codes, request IDs and Retry-After are reported.
	detail := "Request failed without a TerraScale error response. Check network access and service availability. An interrupted mutation may have committed; inspect or import the resource before retrying."
	var api *tb.APIError
	var auth *tsauth.APIError
	var token *tsauth.Error
	var contract *tsauth.ResponseContractError
	if errors.As(err, &contract) {
		detail = "TerraScale answered with a response that does not match the public API contract. Upgrade the provider. A mutation may have committed; inspect or import the resource before retrying."
	}
	status, code, requestID := 0, "", ""
	var retryAfter time.Duration
	switch {
	case errors.As(err, &api):
		status, code, requestID, retryAfter = api.Status, string(api.Code), api.RequestID, api.RetryAfter
	case errors.As(err, &auth):
		status, code, retryAfter = auth.Status, string(auth.Code), auth.RetryAfter
	case errors.As(err, &token):
		status, code, retryAfter = token.Status, token.Code, token.RetryAfter
	}
	if !errorCodePattern.MatchString(code) {
		code = ""
	}
	if status != 0 {
		detail = fmt.Sprintf("TerraScale returned HTTP %d", status)
		if code != "" {
			detail += " " + code
		}
		detail += "."
		if guidance := errorGuidance[code]; guidance != "" {
			detail += " " + guidance
		} else {
			detail += " Check permissions and the requested resource."
		}
		if retryAfter > 0 {
			detail += fmt.Sprintf(" Retry after %s.", retryAfter.Round(time.Second))
		}
		if status >= 500 || status == http.StatusTooManyRequests {
			detail += " A mutation's outcome may be uncertain; inspect or import the resource before retrying."
		}
	}
	if status == http.StatusUnauthorized || (token != nil && (token.Code == "invalid_grant" || token.Code == "invalid_token" || token.Code == "invalid_client")) {
		detail = "The user or project access token expired or was rejected. Supply a current ts-auth user access token for access_token, or replace project_token for collections, and retry. Token values and server details are omitted."
	}
	var netErr net.Error
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		detail = "Request canceled or timed out. A mutation may have committed; inspect or import the resource before retrying."
	}
	if requestIDPattern.MatchString(requestID) {
		detail += " Request ID: " + requestID + "."
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

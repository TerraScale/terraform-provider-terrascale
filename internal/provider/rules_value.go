package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	tb "terrascale.tech/sdk/v1"
)

type rulesType struct{ basetypes.StringType }
type rulesValue struct{ basetypes.StringValue }

func (rulesType) ValueType(context.Context) attr.Value { return rulesValue{} }
func (t rulesType) ValueFromTerraform(ctx context.Context, value tftypes.Value) (attr.Value, error) {
	v, err := t.StringType.ValueFromTerraform(ctx, value)
	if err != nil {
		return nil, err
	}
	s, ok := v.(basetypes.StringValue)
	if !ok {
		return nil, fmt.Errorf("unexpected rules value type %T", v)
	}
	return rulesValue{s}, nil
}
func (rulesType) String() string { return "terrascale.rules" }
func (rulesType) Equal(other attr.Type) bool {
	_, ok := other.(rulesType)
	return ok
}
func (rulesType) ValueFromString(_ context.Context, value basetypes.StringValue) (basetypes.StringValuable, diag.Diagnostics) {
	return rulesValue{value}, nil
}
func (rulesValue) Type(context.Context) attr.Type { return rulesType{} }
func (v rulesValue) Equal(other attr.Value) bool {
	o, ok := other.(rulesValue)
	return ok && v.StringValue.Equal(o.StringValue)
}
func (v rulesValue) StringSemanticEquals(_ context.Context, other basetypes.StringValuable) (bool, diag.Diagnostics) {
	o, ok := other.(rulesValue)
	if !ok {
		return false, nil
	}
	a, err := canonicalRules(v.ValueString())
	if err != nil {
		return false, nil
	}
	b, err := canonicalRules(o.ValueString())
	return err == nil && a == b, nil
}
func decodeRules(value string) (tb.RulesSet, error) {
	var rules tb.RulesSet
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rules); err != nil {
		return rules, errors.New("Use a rules JSON object with tables and supported table rule fields of the documented types.")
	}
	if decoder.Decode(new(any)) != io.EOF || rules.Tables == nil {
		return rules, errors.New("Use exactly one JSON object with a non-null tables object; use {\"tables\":{}} to deny every operation.")
	}
	var document struct {
		Tables map[string]json.RawMessage `json:"tables"`
	}
	if err := json.Unmarshal([]byte(value), &document); err != nil {
		return rules, errors.New("Use a valid rules JSON object.")
	}
	for _, table := range document.Tables {
		if string(table) == "null" {
			return rules, errors.New("Every table rule must be a JSON object, not null.")
		}
	}
	return rules, nil
}
func canonicalRules(value string) (string, error) {
	rules, err := decodeRules(value)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(rules)
	return string(body), err
}

type rulesValidator struct{}

func (rulesValidator) Description(context.Context) string {
	return "A rules JSON object containing tables, up to 256 KiB."
}
func (v rulesValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (rulesValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if len(req.ConfigValue.ValueString()) > 256*1024 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid rules JSON", "Rules must not exceed 256 KiB.")
		return
	}
	if _, err := decodeRules(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid rules JSON", err.Error())
	}
}

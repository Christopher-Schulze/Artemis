package js

import (
	v8 "rogchap.com/v8go"
)

// Value wraps a v8go.Value with type-safe accessors.
type Value struct {
	raw *v8.Value
}

func newValue(raw *v8.Value) *Value { return &Value{raw: raw} }

// Raw returns the underlying v8go value.
func (v *Value) Raw() *v8.Value { return v.raw }

// String returns the string representation of the value.
func (v *Value) String() string {
	if v == nil || v.raw == nil {
		return ""
	}
	return v.raw.String()
}

// Bool returns the boolean coercion of the value.
func (v *Value) Bool() bool {
	if v == nil || v.raw == nil {
		return false
	}
	return v.raw.Boolean()
}

// Int64 returns the integer coercion of the value.
func (v *Value) Int64() int64 {
	if v == nil || v.raw == nil {
		return 0
	}
	return v.raw.Integer()
}

// Float64 returns the floating-point coercion of the value.
func (v *Value) Float64() float64 {
	if v == nil || v.raw == nil {
		return 0
	}
	return v.raw.Number()
}

// IsUndefined reports whether the value is JS undefined.
func (v *Value) IsUndefined() bool {
	return v != nil && v.raw != nil && v.raw.IsUndefined()
}

// IsNull reports whether the value is JS null.
func (v *Value) IsNull() bool {
	return v != nil && v.raw != nil && v.raw.IsNull()
}

// IsObject reports whether the value is a JS object.
func (v *Value) IsObject() bool {
	return v != nil && v.raw != nil && v.raw.IsObject()
}

// IsString reports whether the value is a JS string.
func (v *Value) IsString() bool {
	return v != nil && v.raw != nil && v.raw.IsString()
}

// IsNumber reports whether the value is a JS number.
func (v *Value) IsNumber() bool {
	return v != nil && v.raw != nil && v.raw.IsNumber()
}

// IsBool reports whether the value is a JS boolean.
func (v *Value) IsBool() bool {
	return v != nil && v.raw != nil && v.raw.IsBoolean()
}

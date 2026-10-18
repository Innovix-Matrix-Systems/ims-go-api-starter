// Package settings implements the typed settings catalog, grouped REST surface
// and service. Values resolve tenant override -> global row -> code default.
package settings

import "fmt"

// ValueType describes the storage/validation type of a setting.
type ValueType string

const (
	TypeString ValueType = "string"
	TypeInt    ValueType = "int"
	TypeBool   ValueType = "bool"
	TypeJSON   ValueType = "json"
)

// Scope declares which targets may override a setting. The zero value
// (ScopeGlobalOnly) means no tenant override is legal.
type Scope string

const (
	// ScopeGlobalOnly is the zero value: only the global row (or default) applies.
	ScopeGlobalOnly Scope = ""
	// ScopeTenant allows a per-tenant override.
	ScopeTenant Scope = "tenant"
)

// Key is one typed setting definition.
type Key struct {
	Name        string
	Type        ValueType
	Default     any
	Description string
	Group       string // settings_groups.slug
	SortOrder   int
	Scope       Scope
}

// AllowsTenant reports whether a tenant override is legal for the key.
func (k Key) AllowsTenant() bool {
	return k.Scope == ScopeTenant
}

// Group is one settings group definition (settings_groups row seed).
type Group struct {
	Slug        string
	Name        string
	Description string
	SortOrder   int
}

// Groups is the catalog-owned group set; `settings:sync` upserts them.
var Groups = []Group{
	{Slug: "general", Name: "General", Description: "Core application settings.", SortOrder: 1},
	{Slug: "notifications", Name: "Notifications", Description: "Outbound notification behavior.", SortOrder: 2},
	{Slug: "data-processing", Name: "Data Processing", Description: "Import/export retention and processing.", SortOrder: 3},
}

// Catalog is the application setting catalog; `settings:sync` upserts globals.
var Catalog = []Key{
	{
		Name:        "system.name",
		Type:        TypeString,
		Default:     "IMS Go API",
		Description: "Display name used in notifications and the dashboard.",
		Group:       "general",
		SortOrder:   10,
		Scope:       ScopeGlobalOnly,
	},
	{
		Name:        "notification.enabled",
		Type:        TypeBool,
		Default:     true,
		Description: "Toggles outbound notification fan-out.",
		Group:       "notifications",
		SortOrder:   10,
		Scope:       ScopeTenant,
	},
	{
		Name:        "data_processing_job.cleanup_days",
		Type:        TypeInt,
		Default:     30,
		Description: "Retention in days for completed import/export job records.",
		Group:       "data-processing",
		SortOrder:   10,
		Scope:       ScopeTenant,
	},
}

// Lookup finds a catalog entry by name.
func Lookup(name string) (Key, bool) {
	for _, key := range Catalog {
		if key.Name == name {
			return key, true
		}
	}
	return Key{}, false
}

// LookupGroup finds a catalog group by slug.
func LookupGroup(slug string) (Group, bool) {
	for _, group := range Groups {
		if group.Slug == slug {
			return group, true
		}
	}
	return Group{}, false
}

// Coerce validates and converts a value to the key's type. JSON-decoded
// numbers arrive as float64; direct Go calls may pass int/int64.
func (k Key) Coerce(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch k.Type {
	case TypeString:
		if _, ok := value.(string); !ok {
			return nil, fmt.Errorf("setting %q expects a string", k.Name)
		}
		return value, nil
	case TypeBool:
		if _, ok := value.(bool); !ok {
			return nil, fmt.Errorf("setting %q expects a boolean", k.Name)
		}
		return value, nil
	case TypeInt:
		switch typed := value.(type) {
		case float64:
			return int(typed), nil
		case int:
			return typed, nil
		case int64:
			return int(typed), nil
		default:
			return nil, fmt.Errorf("setting %q expects an integer", k.Name)
		}
	case TypeJSON:
		switch value.(type) {
		case map[string]any, []any, string, float64, bool, int, int64:
			return value, nil
		default:
			return nil, fmt.Errorf("setting %q expects a JSON value", k.Name)
		}
	default:
		return nil, fmt.Errorf("setting %q has unsupported type %q", k.Name, k.Type)
	}
}

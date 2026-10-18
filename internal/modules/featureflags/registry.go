// Package featureflags implements the feature-flag catalog and service. Flags
// resolve user override -> tenant override -> global row -> code default and are
// separate from settings so flag reads never load settings.
package featureflags

// Flag is one boolean feature-flag definition.
type Flag struct {
	Key         string
	Description string
	Default     bool
	Group       string
}

// Catalog is the application feature-flag catalog; `feature-flags:sync` upserts
// globals. Keep flags boolean-only in v1.
var Catalog = []Flag{
	{
		Key:         "feature.audit",
		Description: "Enables audit-log features.",
		Default:     false,
		Group:       "features",
	},
	{
		Key:         "feature.data_processing",
		Description: "Enables background import/export processing.",
		Default:     true,
		Group:       "features",
	},
}

// Lookup finds a catalog entry by key.
func Lookup(key string) (Flag, bool) {
	for _, flag := range Catalog {
		if flag.Key == key {
			return flag, true
		}
	}
	return Flag{}, false
}

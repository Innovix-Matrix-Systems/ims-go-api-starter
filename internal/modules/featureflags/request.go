package featureflags

// UpdateFlagsRequest is the internal batch flag update payload. A nil flag value
// clears that override (inherit).
type UpdateFlagsRequest struct {
	TargetType string           `json:"target_type" validate:"omitempty,oneof=global tenant user"`
	TargetID   *int64           `json:"target_id" validate:"omitempty,gt=0"`
	Flags      map[string]*bool `json:"flags" validate:"required,min=1"`
}

// FlagItem is one effective flag in a response.
type FlagItem struct {
	Key         string `json:"key"`
	Value       bool   `json:"value"`
	Default     bool   `json:"default"`
	Source      string `json:"source"`
	Description string `json:"description"`
	Group       string `json:"group"`
}

func toFlagItems(items []EffectiveFlag) []FlagItem {
	out := make([]FlagItem, 0, len(items))
	for _, item := range items {
		out = append(out, FlagItem{
			Key:         item.Flag.Key,
			Value:       item.Value,
			Default:     item.Flag.Default,
			Source:      item.Source,
			Description: item.Flag.Description,
			Group:       item.Flag.Group,
		})
	}
	return out
}

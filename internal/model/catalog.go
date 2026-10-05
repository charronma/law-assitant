// Package model resolves chat models by id and classifies their failures.
package model

// Model tiers shown to users, from most to least capable.
const (
	TierFlagship = "flagship"
	TierStandard = "standard"
	TierFast     = "fast"
)

// Info describes a selectable model.
type Info struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Tier  string `json:"tier"`
}

// builtin is the catalog of known models in recommended order. It supplies the
// display label and tier for each id; the set that is actually selectable is
// the QWEN_MODELS allow-list, so adding a model later only needs an env change.
var builtin = []Info{
	{"qwen3.8-max-0902", "通义千问 3.8 Max（0902）", TierFlagship},
	{"qwen3.8-max", "通义千问 3.8 Max", TierFlagship},
	{"deepseek-v4-pro-0813", "DeepSeek V4 Pro", TierFlagship},
	{"qwen3.8-2.4t-a95b", "通义千问 3.8 2.4T MoE", TierFlagship},
	{"glm-5.3", "GLM-5.3", TierFlagship},
	{"kimi-k3", "Kimi K3", TierFlagship},
	{"qwen3.8-27b", "通义千问 3.8 27B", TierStandard},
	{"deepseek-v4.1-flash", "DeepSeek V4.1 Flash", TierFast},
	{"qwen3.8-flash", "通义千问 3.8 Flash", TierFast},
	{"qwen3.7-flash", "通义千问 3.7 Flash", TierFast},
	{"qwen3.7-flash-2026-07-15", "通义千问 3.7 Flash（0715）", TierFast},
	{"deepseek-v4-flash-0731", "DeepSeek V4 Flash（0731）", TierFast},
}

// DefaultModelID is the model used when a request does not pick one.
const DefaultModelID = "qwen3.8-max-0902"

// DefaultIDs returns every built-in model id in recommended order.
func DefaultIDs() []string {
	ids := make([]string, len(builtin))
	for i, m := range builtin {
		ids[i] = m.ID
	}
	return ids
}

// Describe returns the display info for id. Unknown ids are still usable: the
// label falls back to the id itself and the tier to "standard".
func Describe(id string) Info {
	for _, m := range builtin {
		if m.ID == id {
			return m
		}
	}
	return Info{ID: id, Label: id, Tier: TierStandard}
}

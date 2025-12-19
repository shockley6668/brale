package types

type Feature struct {
	Key         string
	Label       string
	Value       float64
	Description string
	Metadata    map[string]any
}

type FeatureReport struct {
	Symbol     string   `json:"symbol"`
	Profile    string   `json:"profile"`
	ContextTag string   `json:"context_tag"`
	Lines      []string `json:"lines"`
}

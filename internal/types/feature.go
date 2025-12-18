package types

// Feature 表达一个结构化特征条目。
type Feature struct {
	Key         string
	Label       string
	Value       float64
	Description string
	Metadata    map[string]any
}

// FeatureReport 是 Pipeline 汇总给 Prompt 的语义化特征。
type FeatureReport struct {
	Symbol     string   `json:"symbol"`
	Profile    string   `json:"profile"`
	ContextTag string   `json:"context_tag"`
	Lines      []string `json:"lines"`
}

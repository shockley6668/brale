package livehttp

// SymbolDetail 用于向前端暴露的币种配置概要。
type SymbolDetail struct {
	Profile      string   `json:"profile"`
	Middlewares  []string `json:"middlewares,omitempty"`
	Strategies   []string `json:"strategies,omitempty"`
	ExitSummary  string   `json:"exit_summary,omitempty"`
	ExitCombos   []string `json:"exit_combos,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	UserPrompt   string   `json:"user_prompt,omitempty"`
}

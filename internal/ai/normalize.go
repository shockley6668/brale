package ai

import "strings"

// NormalizeAction 统一动作名称：大小写不敏感，并将 wait 视为 hold
func NormalizeAction(a string) string {
    a = strings.ToLower(strings.TrimSpace(a))
    if a == "wait" {
        return "hold"
    }
    return a
}


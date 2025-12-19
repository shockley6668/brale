package decision

import (
	"fmt"
	"strings"
)

type ThoughtRow struct {
	Provider string
	Thought  string
	Failed   bool
}

type ResultRow struct {
	Provider string
	Action   string
	Symbol   string
	Reason   string
	Failed   bool
}

func RenderThoughtsTable(rows []ThoughtRow, _ int) string {
	if len(rows) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, row := range rows {
		provider := fallback(row.Provider, "-")
		status := ""
		if row.Failed {
			status = " [FAILED]"
		}
		sb.WriteString(fmt.Sprintf("- [%s]%s\n", provider, status))
		content := strings.TrimSpace(row.Thought)
		if content == "" {
			content = "(empty)"
		}
		sb.WriteString(indentLines(content, "  "))
		sb.WriteRune('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

func RenderResultsTable(rows []ResultRow, _ int) string {
	if len(rows) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("=== AI 决策结果 ===\n")
	for _, row := range rows {
		provider := fallback(row.Provider, "-")
		status := ""
		if row.Failed {
			status = " [FAILED]"
		}
		sb.WriteString(fmt.Sprintf("- [%s]%s action=%s symbol=%s\n", provider, status, strings.TrimSpace(row.Action), strings.TrimSpace(row.Symbol)))
		reason := strings.TrimSpace(row.Reason)
		if reason == "" {
			reason = "(no reasoning)"
		}
		sb.WriteString(indentLines(reason, "  "))
		sb.WriteRune('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

func RenderBlockTable(title, content string) string {
	title = fallback(title, "Note")
	body := strings.TrimSpace(content)
	if body == "" {
		body = "(empty)"
	}
	var sb strings.Builder
	sb.WriteString(title)
	sb.WriteRune('\n')
	sb.WriteString(indentLines(body, "  "))
	return strings.TrimRight(sb.String(), "\n")
}

func RenderFinalDecisionsTable(ds []Decision, _ int) string {
	var sb strings.Builder
	sb.WriteString("=== Final Decisions ===\n")
	if len(ds) == 0 {
		sb.WriteString("(none)")
		return sb.String()
	}
	for _, d := range ds {
		sl := "-"
		tp := "-"
		if d.StopLoss > 0 {
			sl = fmt.Sprintf("%.4f", d.StopLoss)
		}
		if d.TakeProfit > 0 {
			tp = fmt.Sprintf("%.4f", d.TakeProfit)
		}
		sb.WriteString(fmt.Sprintf("- action=%s symbol=%s sl=%s tp=%s\n", strings.TrimSpace(d.Action), strings.TrimSpace(d.Symbol), sl, tp))

		reason := strings.TrimSpace(d.Reasoning)
		if reason != "" {
			sb.WriteString(indentLines(reason, "    "))
			sb.WriteRune('\n')
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func indentLines(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func fallback(val, def string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return def
	}
	return val
}

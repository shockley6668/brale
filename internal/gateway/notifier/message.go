package notifier

import (
	"strings"
	"time"
)

const maxStructuredMessageLen = 3800

// MessageSection 表示通知中的一个段落。
type MessageSection struct {
	Title string
	Lines []string
}

// StructuredMessage 描述统一格式的 Telegram 推送。
type StructuredMessage struct {
	Icon      string
	Title     string
	Sections  []MessageSection
	Footer    string
	Timestamp time.Time
}

// RenderMarkdown 生成 Markdown 文本，自动裁剪长度。
func (m StructuredMessage) RenderMarkdown() string {
	var b strings.Builder
	header := strings.TrimSpace(strings.TrimSpace(m.Icon + " " + m.Title))
	if header != "" {
		b.WriteString(header + "\n\n")
	}
	if block := renderSections(m.Sections); block != "" {
		b.WriteString(block)
	}
	if footer := strings.TrimSpace(m.Footer); footer != "" {
		b.WriteString(sanitize(footer))
		b.WriteString("\n")
	}
	if !m.Timestamp.IsZero() {
		b.WriteString("时间：" + m.Timestamp.Format("2006-01-02 15:04:05 MST"))
	}
	body := strings.TrimSpace(b.String())
	if len(body) > maxStructuredMessageLen {
		body = body[:maxStructuredMessageLen] + "..."
	}
	return body
}

func renderSections(secs []MessageSection) string {
	hasContent := false
	for _, sec := range secs {
		if len(sanitizeLines(sec.Lines)) > 0 {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return ""
	}
	var b strings.Builder
	b.WriteString("```\n")
	for idx, sec := range secs {
		lines := sanitizeLines(sec.Lines)
		if len(lines) == 0 {
			continue
		}
		title := strings.TrimSpace(sec.Title)
		if title != "" {
			b.WriteString(sanitize(title))
			b.WriteString("\n")
		}
		for _, line := range lines {
			b.WriteString("- ")
			b.WriteString(sanitize(line))
			b.WriteString("\n")
		}
		if idx != len(secs)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("```\n\n")
	return b.String()
}

func sanitizeLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if text := strings.TrimSpace(line); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, "```", "'''")
	return s
}

package decision

import (
	"fmt"
	"sort"
	"strings"
)

func renderOutputConstraints(profilePrompts map[string]ProfilePromptSpec, exampleLead string) string {
	if len(profilePrompts) == 0 {
		return ""
	}

	symbols := outputConstraintSymbols(profilePrompts)
	if len(symbols) == 0 {
		return ""
	}

	blocks := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		spec, ok := profilePrompts[sym]
		if !ok {
			continue
		}
		blk := outputConstraintBlock(sym, spec, exampleLead)
		if blk == "" {
			continue
		}
		blocks = append(blocks, blk)
	}
	return joinOutputConstraintBlocks(blocks)
}

func outputConstraintSymbols(profilePrompts map[string]ProfilePromptSpec) []string {
	if len(profilePrompts) == 0 {
		return nil
	}
	symbols := make([]string, 0, len(profilePrompts))
	for sym := range profilePrompts {
		key := strings.ToUpper(strings.TrimSpace(sym))
		if key == "" {
			continue
		}
		symbols = append(symbols, key)
	}
	sort.Strings(symbols)
	return symbols
}

func outputConstraintBlock(sym string, spec ProfilePromptSpec, exampleLead string) string {
	userPrompt := strings.TrimSpace(spec.UserPrompt)
	exitText := strings.TrimSpace(spec.ExitConstraints)
	example := strings.TrimSpace(spec.Example)
	if userPrompt == "" && exitText == "" && example == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(outputConstraintHeader(sym, spec) + "\n")
	writeOutputConstraintBody(&b, userPrompt, exitText, example, exampleLead)
	return b.String()
}

func outputConstraintHeader(sym string, spec ProfilePromptSpec) string {
	meta := make([]string, 0, 3)
	if profile := strings.TrimSpace(spec.Profile); profile != "" {
		meta = append(meta, fmt.Sprintf("Profile=%s", profile))
	}
	if tag := strings.TrimSpace(spec.ContextTag); tag != "" {
		meta = append(meta, fmt.Sprintf("Context=%s", tag))
	}
	if ref := strings.TrimSpace(spec.PromptRef); ref != "" {
		meta = append(meta, fmt.Sprintf("Prompt=%s", ref))
	}

	header := fmt.Sprintf("- %s", sym)
	if len(meta) == 0 {
		return header
	}
	return fmt.Sprintf("%s (%s)", header, strings.Join(meta, " | "))
}

func writeOutputConstraintBody(b *strings.Builder, userPrompt, exitText, example, exampleLead string) {
	if b == nil {
		return
	}
	writeOptionalText(b, userPrompt, false)
	writeOptionalText(b, exitText, userPrompt != "")
	if example == "" {
		return
	}
	if userPrompt != "" || exitText != "" {
		b.WriteByte('\n')
	}
	writeOptionalText(b, strings.TrimSpace(exampleLead), false)
	b.WriteString("```json\n")
	b.WriteString(example)
	b.WriteString("\n```\n")
}

func writeOptionalText(b *strings.Builder, content string, leadingBlankLine bool) {
	if b == nil {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	if leadingBlankLine {
		b.WriteByte('\n')
	}
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteByte('\n')
	}
}

func joinOutputConstraintBlocks(blocks []string) string {
	if len(blocks) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\n## 输出约束\n")
	for i, blk := range blocks {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(blk)
	}
	return out.String()
}

package app

import (
	"fmt"
	"sort"
	"strings"
)

type StartupSummary struct {
	KLine         KLineSummary
	EMA           EMASummary
	Prompts       map[string]string
	SymbolDetails map[string]SymbolDetail
}

type SymbolDetail struct {
	ProfileName  string
	Middlewares  []string
	Strategies   []string
	ExitSummary  string
	ExitCombos   []string
	SystemPrompt string
	UserPrompt   string
}

type KLineSummary struct {
	Symbols   []string
	Intervals []string
	MaxCached int
}

type EMASummary struct {
	TargetTimeframes []string
	BasePeriod       string
	HistoryLimit     int
}

func (s *StartupSummary) Print() {
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("%*s\n", 40+len("启动配置摘要 (STARTUP SUMMARY)")/2, "启动配置摘要 (STARTUP SUMMARY)")
	fmt.Println(strings.Repeat("=", 80))

	fmt.Println("[K线数据 (K-LINE DATA)]")
	fmt.Printf("  监控币种: %s\n", formatList(s.KLine.Symbols))
	fmt.Printf("  订阅周期: %s\n", formatList(s.KLine.Intervals))
	fmt.Printf("  最大缓存: %d\n", s.KLine.MaxCached)
	fmt.Println()

	fmt.Println("[EMA / 衍生品配置 (EMA / METRICS)]")
	fmt.Printf("  目标周期: %s\n", formatList(s.EMA.TargetTimeframes))
	fmt.Printf("  基准周期: %s\n", s.EMA.BasePeriod)
	fmt.Printf("  历史长度: %d\n", s.EMA.HistoryLimit)
	fmt.Println()

	fmt.Println("[币种配置详情 (SYMBOLS CONFIGURATION)]")
	if len(s.SymbolDetails) == 0 {
		fmt.Println("  (无配置)")
	} else {

		symbols := make([]string, 0, len(s.SymbolDetails))
		for sym := range s.SymbolDetails {
			symbols = append(symbols, sym)
		}
		sort.Strings(symbols)

		for _, sym := range symbols {
			detail := s.SymbolDetails[sym]
			fmt.Printf("  > %s (配置组: %s)\n", sym, detail.ProfileName)

			fmt.Println("    [中间件]:")
			if len(detail.Middlewares) == 0 {
				fmt.Println("      - (无)")
			} else {
				for _, mw := range detail.Middlewares {
					fmt.Printf("      - %s\n", mw)
				}
			}

			fmt.Println("    [策略组合]:")
			if len(detail.Strategies) == 0 {
				fmt.Println("      - (无)")
			} else {
				for _, st := range detail.Strategies {
					fmt.Printf("      - %s\n", st)
				}
			}
			if detail.ExitSummary != "" {
				fmt.Printf("    [出场摘要]: %s\n", detail.ExitSummary)
			}
			fmt.Println()
		}
	}

	fmt.Println("[提示词与约束 (PROMPTS & CONSTRAINTS)]")
	if len(s.SymbolDetails) == 0 {
		fmt.Println("  (无)")
	} else {
		symbols := make([]string, 0, len(s.SymbolDetails))
		for sym := range s.SymbolDetails {
			symbols = append(symbols, sym)
		}
		sort.Strings(symbols)

		for _, sym := range symbols {
			detail := s.SymbolDetails[sym]
			fmt.Printf("  > %s:\n", sym)

			printPrompt := func(role, name string) {
				content, ok := s.Prompts[name]
				if !ok {
					fmt.Printf("    [%s Prompt] %s: (未找到内容)\n", role, name)
					return
				}
				preview := content
				lines := strings.Split(content, "\n")
				if len(lines) > 5 {
					preview = strings.Join(lines[:5], "\n") + "\n    ... (truncated)"
				}

				preview = strings.ReplaceAll(preview, "\n", "\n    ")
				fmt.Printf("    [%s Prompt] %s:\n    %s\n", role, name, preview)
			}

			printPrompt("System", detail.SystemPrompt)
			printPrompt("User", detail.UserPrompt)
			fmt.Println()
		}
	}
	fmt.Println(strings.Repeat("=", 80))
}

func formatList(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ", ")
}

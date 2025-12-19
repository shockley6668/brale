package logger

import (
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
)

var (
	llmMu          sync.Mutex
	llmLog         *log.Logger
	llmDumpPayload bool
)

func SetLLMWriter(w io.Writer) {
	llmMu.Lock()
	defer llmMu.Unlock()
	if w == nil {
		llmLog = nil
		return
	}
	llmLog = log.New(w, "", log.LstdFlags)
}

type llmSection struct {
	Title string
	Body  string
}

func logLLM(kind, provider, purpose string, sections []llmSection) {
	llmMu.Lock()
	logger := llmLog
	llmMu.Unlock()
	if logger == nil {
		return
	}
	var b strings.Builder
	b.WriteString("[LLM]")
	if kind != "" {
		b.WriteString("[")
		b.WriteString(kind)
		b.WriteString("]")
	}
	if provider != "" {
		b.WriteString("[")
		b.WriteString(provider)
		b.WriteString("]")
	}
	if purpose != "" {
		b.WriteString("[")
		b.WriteString(purpose)
		b.WriteString("]")
	}
	b.WriteString("\n")
	for _, sec := range sections {
		t := strings.TrimSpace(sec.Title)
		if t == "" {
			t = "CONTENT"
		}
		b.WriteString("--- ")
		b.WriteString(t)
		b.WriteString(" ---\n")
		body := sec.Body
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("=====\n")
	logger.Print(b.String())
}

func LogLLMRequest(kind, provider, purpose, systemPrompt, userPrompt string, images []string, payload string) {
	sections := []llmSection{
		{Title: "SYSTEM", Body: systemPrompt},
		{Title: "USER", Body: userPrompt},
	}
	if len(images) > 0 {
		for i, img := range images {
			title := fmt.Sprintf("IMAGE#%d", i+1)
			sections = append(sections, llmSection{Title: title, Body: img})
		}
	}
	if llmDumpPayload && strings.TrimSpace(payload) != "" {
		sections = append(sections, llmSection{Title: "PAYLOAD", Body: payload})
	}
	logLLM(kind+"-request", provider, purpose, sections)
}

func LogLLMResponse(kind, provider, purpose, raw string) {
	sections := []llmSection{{Title: "RAW", Body: raw}}
	logLLM(kind+"-response", provider, purpose, sections)
}

func EnableLLMPayloadDump(enabled bool) {
	llmMu.Lock()
	llmDumpPayload = enabled
	llmMu.Unlock()
}

func LogLLMPayload(provider, payload string) {
	if !llmDumpPayload {
		return
	}
	text := strings.TrimSpace(payload)
	if text == "" {
		return
	}
	sections := []llmSection{{Title: "PAYLOAD", Body: text}}
	logLLM("payload", provider, "request", sections)
}

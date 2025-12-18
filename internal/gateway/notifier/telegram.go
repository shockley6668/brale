package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 中文说明：
// Telegram 通知器：在开仓决策触发时，将关键信息推送至指定群/频道。

type Telegram struct {
	BotToken string
	ChatID   string
	Client   *http.Client
}

func NewTelegram(botToken, chatID string) *Telegram {
	return &Telegram{BotToken: botToken, ChatID: chatID, Client: &http.Client{Timeout: 15 * time.Second}}
}

func (t *Telegram) sendMessage(payload map[string]any) (int, string, error) {
	if t == nil || t.Client == nil {
		return 0, "", fmt.Errorf("telegram client not initialized")
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.BotToken)
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.Client.Do(req)
	if err != nil {
		return 0, "", err
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
	_ = resp.Body.Close()
	return resp.StatusCode, strings.TrimSpace(string(respBody)), nil
}

// SendText 发送文本消息（带最多 3 次重试）
func (t *Telegram) SendText(text string) error {
	if t.BotToken == "" || t.ChatID == "" {
		return fmt.Errorf("Telegram 配置不完整")
	}
	payload := map[string]any{
		"chat_id":    t.ChatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	var lastErr error
	for i := 0; i < 3; i++ {
		status, desc, err := t.sendMessage(payload)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}
		if status/100 == 2 {
			return nil
		}

		// Telegram Markdown is fragile; fall back to plain text when entity parsing fails.
		if status == http.StatusBadRequest && strings.Contains(desc, "can't parse entities") {
			fallback := map[string]any{
				"chat_id": t.ChatID,
				"text":    text,
			}
			if status2, desc2, err2 := t.sendMessage(fallback); err2 == nil && status2/100 == 2 {
				return nil
			} else if err2 != nil {
				lastErr = err2
			} else if strings.TrimSpace(desc2) != "" {
				lastErr = fmt.Errorf("telegram status=%d body=%s", status2, desc2)
			} else {
				lastErr = fmt.Errorf("telegram status=%d", status2)
			}
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}

		if desc != "" {
			lastErr = fmt.Errorf("telegram status=%d body=%s", status, desc)
		} else {
			lastErr = fmt.Errorf("telegram status=%d", status)
		}
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	return lastErr
}

// SendStructured 根据结构化消息渲染并发送 Markdown。
func (t *Telegram) SendStructured(msg StructuredMessage) error {
	return t.SendText(msg.RenderMarkdown())
}

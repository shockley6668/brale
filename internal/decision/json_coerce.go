package decision

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

func CoerceDecisionArrayJSON(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("json 内容为空")
	}
	if !gjson.Valid(raw) {
		return "", fmt.Errorf("json 格式无效")
	}
	parsed := gjson.Parse(raw)
	if parsed.IsArray() {
		return raw, nil
	}
	if !parsed.IsObject() {
		return "", fmt.Errorf("根节点必须是 JSON 数组或对象")
	}
	if decisions := parsed.Get("decisions"); decisions.Exists() {
		if !decisions.IsArray() {
			return "", fmt.Errorf("decisions 必须是数组")
		}
		return strings.TrimSpace(decisions.Raw), nil
	}

	if strings.TrimSpace(parsed.Get("action").String()) == "" {
		return "", fmt.Errorf("根节点为对象但未包含 decisions 数组或 action 字段")
	}
	return "[" + raw + "]", nil
}

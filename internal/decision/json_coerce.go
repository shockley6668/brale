package decision

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// CoerceDecisionArrayJSON 将模型输出中的 JSON 统一转换为“决策数组”字符串：
// - 若根节点为数组：原样返回
// - 若根节点为对象且包含 decisions 数组：返回该数组
// - 若根节点为对象且看起来是单条决策：包装为数组后返回
//
// 背景：部分 OpenAI 兼容接口在启用 response_format=json_object 时会强制顶层为对象，
// 因此需要兼容单对象 / wrapper-object 的输出。
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
	// 单条决策对象：至少应包含 action（其余字段可选）
	if strings.TrimSpace(parsed.Get("action").String()) == "" {
		return "", fmt.Errorf("根节点为对象但未包含 decisions 数组或 action 字段")
	}
	return "[" + raw + "]", nil
}


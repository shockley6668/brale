package decision

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

// ValidateDecisionArray 使用 gjson 快速确认 JSON 数组结构与必备字段。
func ValidateDecisionArray(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("json 内容为空")
	}
	if !gjson.Valid(raw) {
		return fmt.Errorf("json 格式无效")
	}
	parsed := gjson.Parse(raw)
	if !parsed.IsArray() {
		return fmt.Errorf("根节点必须是 JSON 数组")
	}
	count := 0
	validAction := false
	var schemaErr error
	parsed.ForEach(func(_, value gjson.Result) bool {
		count++
		action := strings.TrimSpace(value.Get("action").String())
		if action != "" {
			validAction = true
		}
		if schemaErr != nil {
			return false
		}
		switch strings.ToLower(action) {
		case "open_long", "open_short":
			exitPlan := value.Get("exit_plan")
			if !exitPlan.Exists() || !exitPlan.IsObject() {
				schemaErr = fmt.Errorf("决策#%d 缺少 exit_plan", count)
				return false
			}
			if id := strings.TrimSpace(exitPlan.Get("id").String()); id == "" {
				schemaErr = fmt.Errorf("决策#%d exit_plan.id 必填", count)
				return false
			}
			params := exitPlan.Get("params")
			if !params.Exists() || !params.IsObject() {
				schemaErr = fmt.Errorf("决策#%d exit_plan.params 需为对象", count)
				return false
			}
			if comps := exitPlan.Get("components"); comps.Exists() {
				if !comps.IsArray() {
					schemaErr = fmt.Errorf("决策#%d exit_plan.components 必须是数组", count)
					return false
				}
				compIdx := 0
				comps.ForEach(func(_, comp gjson.Result) bool {
					compIdx++
					if !comp.IsObject() {
						schemaErr = fmt.Errorf("决策#%d 组件#%d 需为对象", count, compIdx)
						return false
					}
					if id := strings.TrimSpace(comp.Get("id").String()); id == "" {
						schemaErr = fmt.Errorf("决策#%d 组件#%d 缺少 id", count, compIdx)
						return false
					}
					if p := comp.Get("params"); p.Exists() && !p.IsObject() {
						schemaErr = fmt.Errorf("决策#%d 组件#%d params 需为对象", count, compIdx)
						return false
					}
					return true
				})
				if schemaErr != nil {
					return false
				}
			}
		}
		return true
	})
	if schemaErr != nil {
		return schemaErr
	}
	if count == 0 {
		return fmt.Errorf("决策数组为空")
	}
	if !validAction {
		return fmt.Errorf("未检测到 action 字段")
	}
	return nil
}

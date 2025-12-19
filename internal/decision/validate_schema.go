package decision

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

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
	count, validAction, schemaErr := walkDecisions(parsed)
	switch {
	case schemaErr != nil:
		return schemaErr
	case count == 0:
		return fmt.Errorf("决策数组为空")
	case !validAction:
		return fmt.Errorf("未检测到 action 字段")
	default:
		return nil
	}
}

func walkDecisions(parsed gjson.Result) (count int, validAction bool, schemaErr error) {
	parsed.ForEach(func(_, value gjson.Result) bool {
		count++
		action := strings.TrimSpace(value.Get("action").String())
		if action != "" {
			validAction = true
		}
		if schemaErr != nil {
			return false
		}
		if err := validateDecisionNode(count, action, value); err != nil {
			schemaErr = err
			return false
		}
		return true
	})
	return
}

// validateDecisionNode checks a single decision object.
// Example: open_long requires exit_plan.id+params; components (if present) must be array of objects with id.
func validateDecisionNode(idx int, action string, value gjson.Result) error {
	switch strings.ToLower(action) {
	case "open_long", "open_short":
		exitPlan := value.Get("exit_plan")
		if !exitPlan.Exists() || !exitPlan.IsObject() {
			return fmt.Errorf("决策#%d 缺少 exit_plan", idx)
		}
		if id := strings.TrimSpace(exitPlan.Get("id").String()); id == "" {
			return fmt.Errorf("决策#%d exit_plan.id 必填", idx)
		}
		params := exitPlan.Get("params")
		if !params.Exists() || !params.IsObject() {
			return fmt.Errorf("决策#%d exit_plan.params 需为对象", idx)
		}
		if comps := exitPlan.Get("components"); comps.Exists() {
			return validateExitComponents(idx, comps)
		}
	}
	return nil
}

func validateExitComponents(idx int, comps gjson.Result) error {
	if !comps.IsArray() {
		return fmt.Errorf("决策#%d exit_plan.components 必须是数组", idx)
	}
	compIdx := 0
	var schemaErr error
	comps.ForEach(func(_, comp gjson.Result) bool {
		compIdx++
		if !comp.IsObject() {
			schemaErr = fmt.Errorf("决策#%d 组件#%d 需为对象", idx, compIdx)
			return false
		}
		if id := strings.TrimSpace(comp.Get("id").String()); id == "" {
			schemaErr = fmt.Errorf("决策#%d 组件#%d 缺少 id", idx, compIdx)
			return false
		}
		if p := comp.Get("params"); p.Exists() && !p.IsObject() {
			schemaErr = fmt.Errorf("决策#%d 组件#%d params 需为对象", idx, compIdx)
			return false
		}
		return true
	})
	return schemaErr
}

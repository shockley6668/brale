package decision

import (
	"encoding/json"
	"fmt"
	"strings"

	"brale/internal/exitplan"
	"brale/internal/pkg/jsonutil"
)

type Parser struct {
	ExitPlans *exitplan.Registry
}

func NewParser(exitPlans *exitplan.Registry) *Parser {
	return &Parser{
		ExitPlans: exitPlans,
	}
}

func (p *Parser) Parse(raw string) (DecisionResult, error) {
	parsed := DecisionResult{
		RawOutput: raw,
	}

	block, ok := jsonutil.ExtractJSON(raw)
	if !ok {
		return parsed, fmt.Errorf("未找到 JSON 决策数组")
	}

	arr, err := CoerceDecisionArrayJSON(block)
	if err != nil {
		parsed.RawJSON = strings.TrimSpace(block)
		return parsed, err
	}

	if qerr := ValidateDecisionArray(arr); qerr != nil {
		parsed.RawJSON = arr
		return parsed, qerr
	}

	var ds []Decision
	dec := json.NewDecoder(strings.NewReader(arr))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ds); err != nil {
		parsed.RawJSON = arr
		return parsed, err
	}

	parsed.RawJSON = arr
	if verr := p.validateExitPlans(ds); verr != nil {
		return parsed, verr
	}

	parsed.Decisions = ds
	return parsed, nil
}

func (p *Parser) validateExitPlans(decisions []Decision) error {
	if p.ExitPlans == nil {
		return nil
	}
	for idx := range decisions {
		d := &decisions[idx]
		action := strings.ToLower(strings.TrimSpace(d.Action))
		if action != "open_long" && action != "open_short" {
			continue
		}
		if d.ExitPlan == nil || strings.TrimSpace(d.ExitPlan.ID) == "" {
			return fmt.Errorf("决策#%d 缺少 exit_plan", idx+1)
		}
		tpl, err := p.ExitPlans.Validate(d.ExitPlan.ID, d.ExitPlan.Params)
		if err != nil {
			return fmt.Errorf("决策#%d exit_plan=%s 校验失败: %w", idx+1, d.ExitPlan.ID, err)
		}
		d.ExitPlanVersion = tpl.Version
	}
	return nil
}

package decision

import (
	"encoding/json"
	"fmt"
	"strings"

	"brale/internal/exitplan"
	"brale/internal/pkg/jsonutil"
)

// Parser handles the parsing and validation of model outputs.
type Parser struct {
	ExitPlans *exitplan.Registry
}

// NewParser creates a new output parser.
func NewParser(exitPlans *exitplan.Registry) *Parser {
	return &Parser{
		ExitPlans: exitPlans,
	}
}

// Parse extracts decision from raw model output.
// Note: Dispatcher returns []ModelOutput, but Engine might call Parse on each raw string?
// Or we parse inside Dispatcher?
// In legacy, `callProvider` parses. In standard design, we separated concerns.
// Dispatcher returns raw output in ModelOutput (Parsed field is empty).
// Engine then calls Parser using the raw output.
// Wait, StandardEngine does Aggregation which expects parsed results.
// So StandardEngine loop:
// 1. Dispatch -> []ModelOutput (Raw)
// 2. Loop over outputs -> Parse -> []ModelOutput (Parsed filled)
// 3. Aggregate
//
// Let's implement Parse method.
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
		parsed.RawJSON = arr // Keep JSON even if decode fails partially
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

// ValidateDecisionArray is likely defined in decision/validate.go or similar.
// I saw validate.go in file list.
// If it's exported, we are good.

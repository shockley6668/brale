# Exit Plan 策略约束汇总

本文记录目前组合式退出策略在代码层面的硬性约束，方便编写 Prompt 或构造 Mock 数据时遵守相同规范，避免 AI 输出被判定为“不合规决策”。

## 1. plan_combo_main（`combo_group` handler）

- **唯一计划**：对外仅暴露 `plan_combo_main`，其参数是若干 `children` 的组合。
- **组件别名**：`component` 只能取 `tp_single`、`tp_tiers`、`tp_atr`、`sl_single`、`sl_tiers`、`sl_atr` 六种之一，并且同计划内不得重复。
- **handler 白名单**：对应 handler 仅允许 `tier_take_profit`、`tier_stop_loss`、`atr_trailing`。若传入其他 ID 会直接拒绝。
- **数量与覆盖**：至少 2 个组件，且必须同时包含“止盈类”与“止损类”，避免出现只可止盈或只可止损的计划。
- **终值字段**：若额外配置 `final_take_profit_price / final_stop_loss_price`，应满足多头 `final_take_profit > entry`、`final_stop_loss < entry`（空头反之），系统会在 handler 内再次校验。

## 2. tier_take_profit（分段止盈 handler）

- **绝对价格**：`tiers[*].target_price` 必须是绝对价。多头要求严格递增且 > entry，空头要求严格递减且 < entry。
- **段数与比例**：支持 1–3 段。每段 `ratio > 0`，所有段合计需等于 1（允许 1e-6 的浮动）。
- **最小间隔**：相邻段的目标价需要至少相差 0.2%，最大涨幅限制为 +50%（防止给出过远的目标）。
- **状态锁定**：当段位已触发（status=triggered/done）后会被锁定，`update_exit_plan` 不允许修改该段的价格或比例。

## 3. tier_stop_loss（分段止损 handler）

- **绝对价格**：多头止损价必须严格递减且低于 entry；空头止损价严格递增且高于 entry。
- **段数与比例**：同样允许 1–3 段，比例合计为 1。
- **间距限制**：要求相邻 stop 至少相差 0.1%，最深止损不得超过 -20%（即价格跌幅不超过 20%）。
- **状态锁定**：已触发/已完成的止损段不可再被 update 覆写；仅 waiting/pending 状态允许修改。

## 4. atr_trailing（ATR 追踪模块）

- **模式区分**：新增 `mode` 参数，`take_profit` 表示激活后沿趋势抬升/压低止损锁盈；`stop_loss` 表示用于动态止损。
- **ATR 输入**：`atr_value > 0`；`trigger_multiplier` ∈ [1.0, 5.0]；`trail_multiplier` ≥ 0.5 且 < `trigger_multiplier`。`mode=stop_loss` 可允许更紧的触发倍数（例如 1.2–2.5），但依旧需要 `trail_multiplier < trigger_multiplier`。
- **初始止损**：可选 `initial_stop_multiplier >= 1`，用于在 ATR 触发前设定保护价；若缺失则默认沿用计划根部的 stop。
- **状态锁定**：触发一次后即视为完成，后续 update 不允许再修改该模块。

## 使用建议

1. **Prompt 中强调约束**：请在提示词中明确 target_price 必须严格递增/递减、比例加总为 1、ATR 倍数合法等信息，避免 AI 输出被拒绝。
2. **Mock 数据参照**：测试环境构造计划时，务必复用上述合法区间，尤其是绝对价格的方向性与止损深度。
3. **Profile 附加规则**：若某个 profile 需要更严的限制（如最大止损 3%），可在 `validateExitPlan` 中按 profile name 追加校验；本文列出的是“系统级”强约束。
4. **策略更新 (`update_exit_plan`)**：
   - LLM 在调整策略时必须返回 **完整** 的 `exit_plan` JSON（根节点 + 全部 children），系统会逐段对比后落地。
   - 只有 waiting/pending 的组件允许被替换；若 LLM 试图修改已完成段，整条更新会被拒绝。
   - 更新成功后 PlanScheduler 会写入 `strategy_change_log` 并通知 Trader/Freqtrade，使前端立刻展示最新策略。

如需新增 handler，请同步修改其 `Validate` 逻辑并在本文补充说明，以保持 Prompt/测试/运行时的认知一致。

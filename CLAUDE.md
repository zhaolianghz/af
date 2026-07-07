# CLAUDE.md

AF Selector — A 股选股系统。Go 1.25 (Gin) 后端 + React 18 (Vite/TS) 前端 + akshare sidecar。

- 后端：`backend/`（`cmd/server` 入口，`internal/*` 模块），`go test ./... -race`
- 前端：`frontend/`（`npm run dev` / `typecheck` / `lint` / `test` / `e2e`）
- 系统梳理文档：`docs/plans/2026-07-02-af-system-closed-loop-design.md`

## Skill routing

When the user's request matches an available skill, invoke it via the Skill tool. When in doubt, invoke the skill.

Key routing rules:
- Product ideas/brainstorming → invoke /office-hours
- Strategy/scope → invoke /plan-ceo-review
- Architecture → invoke /plan-eng-review
- Design system/plan review → invoke /design-consultation or /plan-design-review
- Full review pipeline → invoke /autoplan
- Bugs/errors → invoke /investigate
- QA/testing site behavior → invoke /qa or /qa-only
- Code review/diff check → invoke /review
- Visual polish → invoke /design-review
- Ship/deploy/PR → invoke /ship or /land-and-deploy
- Save progress → invoke /context-save
- Resume context → invoke /context-restore
- Author a backlog-ready spec/issue → invoke /spec

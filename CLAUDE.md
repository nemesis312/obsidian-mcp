# CLAUDE.md

<!-- bc-agentic-managed-version: 15 -->

<!-- bc-agentic-managed:start -->
This project uses bc-agentic-os.
## bc-agentic-stack-selection
When project type is auto, infer stack/language from the user goal and repository context during run steps.
Keep stack decisions in PRD/Design/Plan artifacts instead of locking them during `init`.
For new `.NET API` requests, bootstrap from GitHub template `nemesis312/bc-template-dotnet-api` by default (required unless user explicitly opts out).
## bc-agentic-run-protocol-safe-default
When the user says: `Comenzamos con el run <runId>` you MUST:
1) Run: `bc-agentic next <runId>`
2) Edit the returned file path from `bc-agentic next <runId>` (repo docs/agentic working copy) using MCP templates + role instructions
3) Subagents run by default when you call `bc-agentic next <runId>`; manually use `bc-agentic subagent start ...` only if needed
4) Complete subagent handoff: `bc-agentic subagent handoff <runId> <explore|prd|spec|design|tasks|plan|verify> <sessionId> "summary"`
5) In safe/apply mode, ask for approval and if the user confirms in natural language (for example: approved/aprobado/continue), run: `bc-agentic approve <runId> explore|prd|spec|design|tasks|plan|verify`
   In god mode, do not wait for manual approval; continue to the next step after subagent handoff.
6) Repeat until done
Default: safe mode. To execute later on the same run, switch mode with `bc-agentic mode <runId> apply|god`.
## bc-agentic-execution-rules-strict
- Use `resolve_library_id` and `query_library_docs` for up-to-date library/API docs
- Enforce SDD gate before execution (no branch/exec without approved explore+prd+spec+design+tasks+plan and completed subagent handoffs)
## bc-agentic-github-rules
- Before GitHub actions, verify MCP availability with `github_get_me`
- If GitHub MCP is unavailable or errors, explicitly say so before proceeding
- When possible, use `gh` CLI fallback and clearly label it
- Template repos are snapshots: updates affect future repos only; existing repos need explicit sync
## bc-agentic-memory-cadence-required
- Re-run `mem_search` (bc-dev-memory MCP) when new symptoms, constraints, or bug details appear
- Save checkpoints with `mem_save` (bc-dev-memory MCP) at major transitions
- If the user reports a bug/incident, save cause, impact, fix, and prevention notes
- bc-agentic-os MCP is for orchestration only (agents, templates, workflow); use bc-dev-memory for persistence
## bc-agentic-guidance
- Prefer tool-driven context over guessing
- Keep responses scoped to the current goal
- Artifact working files live in repo docs/agentic; use paths returned by `bc-agentic next`
<!-- bc-agentic-managed:end -->

# Plan 006: Prompt Self-Evolution

**Date:** 2026-04-13
**Priority:** P3
**Status:** Draft
**Reference:** [hermes-agent-self-evolution](https://github.com/NousResearch/hermes-agent-self-evolution) (DSPy + GEPA)

## Problem Statement

RingClaw has 5 centralized prompts in `messaging/prompts.go` (ActionPrompt, IntentPrompt, NameExtractPrompt, SummaryPrompt, HeartbeatPrompt). These are hand-tuned with no systematic evaluation. Failures are discovered reactively through user reports:

- IntentPrompt: "总结 John 的代码" misclassified as "summarize" instead of "chat"
- NameExtractPrompt: "总结 maxwell 上周的" extracted "maxwell 上周" (included time word)
- ActionPrompt: agent used person ID as chatid despite explicit rules
- ActionPrompt: agent chose wrong ACTION type for the request

There is no way to:
1. Measure prompt quality before/after a change
2. Catch regressions when modifying prompts
3. Systematically improve prompts using real failure data

## Goals

1. Build an eval harness that scores prompt quality against golden test cases
2. Provide baseline scores for ActionPrompt and IntentPrompt
3. Enable data-driven prompt iteration (change → measure → compare)
4. (Phase 2) Automate prompt mutation with LLM-based proposal + constraint gates

## Non-Goals

- Adding Python/DSPy as a dependency (stay pure Go)
- Mining session history from external tools (RingClaw doesn't store local history)
- Evolving code (only prompt text)
- Auto-committing evolved prompts (always human review via PR)
- Real-time/production prompt evolution (offline tool only)

## Background: Hermes GEPA Approach

The [hermes-agent-self-evolution](https://github.com/NousResearch/hermes-agent-self-evolution) project evolves prompt artifacts using:

1. **DSPy + GEPA optimizer** — wraps prompt text as a parameterized module, mutates via genetic-Pareto evolution
2. **LLM-as-judge scoring** — evaluates on 3 dimensions: correctness (0.5), procedure-following (0.3), conciseness (0.2)
3. **Constraint gates** — size ≤15KB, growth ≤20% over baseline, test suite must pass 100%
4. **Execution trace reflection** — GEPA reads *why* things failed, not just that they failed, to propose targeted mutations
5. **Multi-source eval data** — synthetic (LLM-generated), sessiondb (mined from Claude/Copilot history), golden (hand-curated)

Key insight: the expensive part (LLM-as-judge + GEPA) runs offline via API calls, no GPU needed, ~$2-10 per run.

## Architecture

### Phase 1: Eval Harness + Golden Datasets

```
golden.jsonl ──► eval_prompt.go ──► Agent (ACP) ──► LLM-as-Judge ──► Score Report
                      │                                                    │
                      └── Load prompt from prompts.go ──────────────────────┘
```

**Components:**

| Component | Path | Description |
|-----------|------|-------------|
| Eval runner | `scripts/eval_prompt.go` | CLI tool: loads prompt, runs test cases, scores, reports |
| Golden datasets | `datasets/prompts/<name>/golden.jsonl` | Hand-curated (task_input, expected_behavior, difficulty) |
| Score report | stdout + `output/prompt-eval/<name>/report.json` | Per-case scores + aggregate |

**Golden dataset format** (`golden.jsonl`):
```json
{"task_input": "总结 John 的代码", "expected_behavior": "Classify as 'chat' (not 'summarize') because this asks about code, not chat messages", "difficulty": "hard", "category": "boundary"}
{"task_input": "总结一下最近的消息", "expected_behavior": "Classify as 'summarize' because this asks for chat message summary", "difficulty": "easy", "category": "basic"}
```

**Scoring** (adapted from Hermes `FitnessScore`):
- correctness (weight 0.5): did the agent produce the expected output?
- procedure_following (weight 0.3): did it follow the prompt's instructions?
- conciseness (weight 0.2): was the response appropriately concise?
- length_penalty: ramps from 0 at 90% size to 0.3 at 100%+ size

**Constraint gates:**
- Prompt size ≤ 15,000 chars
- Growth ≤ 20% over baseline
- All existing tests pass (`go test ./messaging/...`)

**Usage:**
```bash
go run scripts/eval_prompt.go --prompt intent --agent claude --iterations 1
go run scripts/eval_prompt.go --prompt action --agent claude --compare prompts/action_v2.md
```

### Phase 2: Automated Mutation (Future)

```
Failing traces ──► Mutation LLM ──► Candidate prompt ──► Eval harness ──► Keep if better
                        │                                      │
                        └──── Constraint gates ◄───────────────┘
```

1. Collect failing test cases + execution traces from Phase 1
2. Send to LLM: "Here is the current prompt, here are the failures. Propose an improved version."
3. Run improved prompt through eval harness
4. If score improves AND constraints pass → save as candidate
5. Repeat N iterations, keep best variant
6. Output diff for human review

## Prompt Evolution Priority

| Prompt | Failure Modes | Evolution Value | Phase |
|--------|---------------|-----------------|-------|
| **ActionPrompt** | Wrong action type, person ID as chatid, missing fields | HIGH | 1 |
| **IntentPrompt** | Boundary cases (code summary vs chat summary) | HIGH | 1 |
| **NameExtractPrompt** | Time words included, partial names | MEDIUM | 1 |
| **SummaryPrompt** | Works well, few complaints | LOW | 2 |
| **HeartbeatPrompt** | Works well | LOW | 2 |

## Implementation

### Phase 1 (~200 LOC Go + JSONL files)

| Step | Files | Description |
|------|-------|-------------|
| 1 | `datasets/prompts/intent/golden.jsonl` | 15-20 hand-curated intent classification cases |
| 2 | `datasets/prompts/action/golden.jsonl` | 15-20 hand-curated ACTION generation cases |
| 3 | `scripts/eval_prompt.go` | Eval runner: load dataset → run agent → LLM judge → report |
| 4 | docs | Update this plan status |

### Phase 2 (~400 LOC Go, future)

| Step | Files | Description |
|------|-------|-------------|
| 1 | `scripts/mutate_prompt.go` | Mutation engine: read traces → propose changes → re-eval |
| 2 | `scripts/eval_prompt.go` | Add `--evolve` flag for automated iteration |
| 3 | Constraint validation | Size/growth checks + `go test` gate |

## Open Questions

1. Should we use the same LLM for judging and for the agent under test? (Hermes uses separate models)
2. How many golden examples are needed for meaningful signal? (Hermes uses 20, split 50/25/25)
3. Should eval results be tracked in git for regression detection?

# Prompt Evaluation Datasets

Golden test cases for evaluating RingClaw's prompts. See [Plan 006](../../docs/plan/006-prompt-self-evolution.md).

## Format

Each line is a JSON object with:

| Field | Required | Description |
|-------|----------|-------------|
| `task_input` | yes | The user message to test |
| `expected_behavior` | yes | What the correct output should be (exact value for intent/name, rubric for action) |
| `difficulty` | yes | `easy`, `medium`, or `hard` |
| `category` | yes | What aspect this tests (e.g. `boundary`, `compound`, `time_word`) |
| `source_pr` | no | Which PR fixed this case (for traceability) |
| `note` | no | Explanation of why this case is tricky |

## Datasets

| Dataset | Prompt | Cases | Description |
|---------|--------|-------|-------------|
| `intent/golden.jsonl` | IntentPrompt | 30 | Classify user intent (summarize/task/note/event/chat) |
| `action/golden.jsonl` | ActionPrompt | 20 | Generate correct ACTION blocks |
| `name_extract/golden.jsonl` | NameExtractPrompt | 20 | Extract person name from summarize request |

## Sources

Test cases are derived from:
1. Historical bug-fix PRs (#34, #40, #62, #68) — real failures
2. Boundary cases from existing unit tests
3. Hand-crafted edge cases for known weak spots

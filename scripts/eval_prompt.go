// eval_prompt evaluates RingClaw prompts against golden test datasets using DeepSeek LLM.
//
// Usage:
//
//	go run scripts/eval_prompt.go --prompt intent
//	go run scripts/eval_prompt.go --prompt name_extract
//	go run scripts/eval_prompt.go --prompt action
//	go run scripts/eval_prompt.go --prompt intent --compare path/to/new_prompt.md
//
// Environment:
//
//	LLM_API_KEY  — required for LLM evaluation
//	LLM_BASE_URL — optional (default: https://api.deepseek.com)
//	LLM_MODEL    — optional (default: deepseek-chat)
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type testCase struct {
	TaskInput        string `json:"task_input"`
	ExpectedBehavior string `json:"expected_behavior"`
	Difficulty       string `json:"difficulty"`
	Category         string `json:"category"`
	SourcePR         string `json:"source_pr,omitempty"`
	Note             string `json:"note,omitempty"`
}

type caseResult struct {
	TaskInput string  `json:"task_input"`
	Expected  string  `json:"expected"`
	Got       string  `json:"got"`
	Pass      bool    `json:"pass"`
	Diff      string  `json:"difficulty"`
	Category  string  `json:"category"`
	Score     float64 `json:"score"`
}

type evalReport struct {
	Prompt    string            `json:"prompt"`
	Model     string            `json:"model"`
	Timestamp string            `json:"timestamp"`
	Total     int               `json:"total"`
	Passed    int               `json:"passed"`
	Failed    int               `json:"failed"`
	Score     float64           `json:"score"`
	ByDiff    map[string][2]int `json:"by_difficulty"`
	ByCat     map[string][2]int `json:"by_category"`
	Results   []caseResult      `json:"results"`
	CompareTo string            `json:"compare_to,omitempty"`
}

// --- DeepSeek API client ---

type llmClient struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func newLLMClient() *llmClient {
	loadDotEnv()

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		return nil
	}

	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "deepseek-chat"
	}

	return &llmClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *llmClient) chat(system, user string) (string, error) {
	msgs := []chatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}

	body, _ := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    msgs,
		Temperature: 0,
		MaxTokens:   256,
	})

	endpoint := c.baseURL + "/v1/chat/completions"
	// If base URL already contains /chat/completions, use it directly
	if strings.Contains(c.baseURL, "/chat/completions") {
		endpoint = c.baseURL
	}
	req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result chatResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

// loadDotEnv reads .env file from current directory (simple key=value format)
func loadDotEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
}

// --- Prompt templates (mirrors messaging/prompts.go) ---

const defaultIntentPrompt = `Classify the user's PRIMARY intent. Reply with ONLY one word:
- "summarize" if the user wants to summarize CHAT HISTORY or MESSAGES (even if they also want to send/note/task the result)
- "task" if the PRIMARY goal is to CREATE a task/todo/action item
- "note" if the PRIMARY goal is to CREATE a note (not just send results as a note)
- "event" if the PRIMARY goal is to CREATE a calendar event/meeting
- "chat" if this is a normal conversation, question, or any other request (including asking an AI to summarize code, documents, or articles)

IMPORTANT: If the message contains BOTH "summarize" AND another action (create note/task/send), the primary intent is ALWAYS "summarize".`

const defaultNameExtractPrompt = `Extract the target person's name from this message.
Reply with ONLY the person's name (e.g. "John Smith"), nothing else.
If no specific person is mentioned, reply with "NONE".`

const defaultActionPrompt = `You are a RingCentral Team Messaging bot with real API actions.
Do NOT generate files or suggest manual steps — use ACTION blocks.

## Available Actions

ACTION:MESSAGE chatid=<name or chat ID>
<message>
END_ACTION

ACTION:NOTE title=<title> [chatid=...]
<body>
END_ACTION

ACTION:TASK subject=<subject> [assignee=<name>] [chatid=...]
<optional description>
END_ACTION

ACTION:EVENT title=<title> start=<ISO8601> end=<ISO8601>
END_ACTION

ACTION:CARD [chatid=...]
<Adaptive Card JSON v1.3>
END_ACTION

## Rules
- chatid: person name (e.g. John Smith), numeric chat ID, or ![:Team](ID). Omit to use current chat.
- assignee: person name or ![:Person](ID).
- The system resolves names to IDs automatically. NEVER use person/creator/user IDs as chatid.
- For structured data, reports, or progress → use ACTION:CARD.
- If no action needed, reply normally without ACTION blocks.`

// judgePrompt scores an agent response against expected behavior
const judgePrompt = `You are an evaluator. Score the agent's response against the expected behavior.
Reply with ONLY a JSON object: {"pass": true/false, "score": 0.0-1.0, "reason": "brief explanation"}

Scoring criteria:
- 1.0: Fully matches expected behavior
- 0.7-0.9: Mostly correct with minor issues
- 0.3-0.6: Partially correct
- 0.0-0.2: Wrong or missing

Expected behavior: %s

Agent response: %s`

func main() {
	promptName := flag.String("prompt", "", "Prompt to evaluate: intent, name_extract, action")
	compareTo := flag.String("compare", "", "Path to alternative prompt file to compare against")
	datasetDir := flag.String("dataset", "", "Override dataset directory (default: datasets/prompts/<prompt>/)")
	outputDir := flag.String("output", "output/prompt-eval", "Output directory for report JSON")
	flag.Parse()

	if *promptName == "" {
		fmt.Fprintln(os.Stderr, "Usage: go run scripts/eval_prompt.go --prompt <intent|name_extract|action>")
		os.Exit(1)
	}

	llm := newLLMClient()
	if llm == nil {
		fmt.Fprintln(os.Stderr, "Error: LLM_API_KEY not set (check .env or environment)")
		os.Exit(1)
	}

	dsDir := *datasetDir
	if dsDir == "" {
		dsDir = filepath.Join("datasets", "prompts", *promptName)
	}

	cases, err := loadGoldenDataset(filepath.Join(dsDir, "golden.jsonl"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading dataset: %v\n", err)
		os.Exit(1)
	}

	// Load custom prompt if --compare is specified
	systemPrompt := ""
	switch *promptName {
	case "intent":
		systemPrompt = defaultIntentPrompt
	case "name_extract":
		systemPrompt = defaultNameExtractPrompt
	case "action":
		systemPrompt = defaultActionPrompt
	default:
		fmt.Fprintf(os.Stderr, "Unknown prompt: %s (use intent, name_extract, or action)\n", *promptName)
		os.Exit(1)
	}

	if *compareTo != "" {
		data, err := os.ReadFile(*compareTo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading compare file: %v\n", err)
			os.Exit(1)
		}
		systemPrompt = string(data)
		fmt.Printf("Using custom prompt from: %s (%d chars)\n\n", *compareTo, len(systemPrompt))
	}

	fmt.Printf("=== %s Evaluation ===\n", *promptName)
	fmt.Printf("Model: %s | Cases: %d\n\n", llm.model, len(cases))

	report := evalReport{
		Prompt:    *promptName,
		Model:     llm.model,
		Timestamp: time.Now().Format(time.RFC3339),
		ByDiff:    make(map[string][2]int),
		ByCat:     make(map[string][2]int),
		CompareTo: *compareTo,
	}

	for i, tc := range cases {
		var result caseResult
		switch *promptName {
		case "intent":
			result = evalIntent(llm, tc, systemPrompt)
		case "name_extract":
			result = evalNameExtract(llm, tc, systemPrompt)
		case "action":
			result = evalAction(llm, tc, systemPrompt)
		}

		report.Results = append(report.Results, result)
		report.Total++
		if result.Pass {
			report.Passed++
		} else {
			report.Failed++
		}

		d := report.ByDiff[tc.Difficulty]
		d[0]++
		if result.Pass {
			d[1]++
		}
		report.ByDiff[tc.Difficulty] = d

		c := report.ByCat[tc.Category]
		c[0]++
		if result.Pass {
			c[1]++
		}
		report.ByCat[tc.Category] = c

		status := "PASS"
		if !result.Pass {
			status = "FAIL"
		}
		fmt.Printf("[%s] %d/%d %q → %q (expected: %q)\n", status, i+1, len(cases),
			truncate(tc.TaskInput, 40), truncate(result.Got, 30), truncate(tc.ExpectedBehavior, 30))
	}

	if report.Total > 0 {
		report.Score = float64(report.Passed) / float64(report.Total) * 100
	}

	fmt.Printf("\n--- Results ---\n")
	fmt.Printf("Score: %d/%d (%.1f%%)\n", report.Passed, report.Total, report.Score)
	for _, diff := range []string{"easy", "medium", "hard"} {
		if d, ok := report.ByDiff[diff]; ok {
			fmt.Printf("  %s: %d/%d\n", diff, d[1], d[0])
		}
	}

	outPath := filepath.Join(*outputDir, *promptName)
	if err := os.MkdirAll(outPath, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot create output dir: %v\n", err)
	} else {
		reportFile := filepath.Join(outPath, "report.json")
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(reportFile, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cannot write report: %v\n", err)
		} else {
			fmt.Printf("\nReport saved to %s\n", reportFile)
		}
	}
}

// evalIntent calls LLM with IntentPrompt and compares reply to expected (exact match)
func evalIntent(llm *llmClient, tc testCase, prompt string) caseResult {
	reply, err := llm.chat(prompt, fmt.Sprintf("User message: %s\n\nIntent:", tc.TaskInput))
	if err != nil {
		return caseResult{TaskInput: tc.TaskInput, Expected: tc.ExpectedBehavior, Got: "[error: " + err.Error() + "]", Diff: tc.Difficulty, Category: tc.Category}
	}

	got := strings.ToLower(strings.Trim(reply, `"' `))
	// Extract first word in case model adds explanation
	if fields := strings.Fields(got); len(fields) > 0 {
		got = fields[0]
	}
	pass := got == strings.ToLower(strings.TrimSpace(tc.ExpectedBehavior))
	score := 0.0
	if pass {
		score = 1.0
	}
	return caseResult{TaskInput: tc.TaskInput, Expected: tc.ExpectedBehavior, Got: got, Pass: pass, Diff: tc.Difficulty, Category: tc.Category, Score: score}
}

// evalNameExtract calls LLM with NameExtractPrompt and compares (exact match, case-insensitive)
func evalNameExtract(llm *llmClient, tc testCase, prompt string) caseResult {
	reply, err := llm.chat(prompt, fmt.Sprintf("Message: %s\n\nName:", tc.TaskInput))
	if err != nil {
		return caseResult{TaskInput: tc.TaskInput, Expected: tc.ExpectedBehavior, Got: "[error: " + err.Error() + "]", Diff: tc.Difficulty, Category: tc.Category}
	}

	got := strings.ToLower(strings.Trim(reply, `"' `))
	expected := strings.ToLower(strings.TrimSpace(tc.ExpectedBehavior))
	pass := got == expected
	score := 0.0
	if pass {
		score = 1.0
	}
	return caseResult{TaskInput: tc.TaskInput, Expected: tc.ExpectedBehavior, Got: got, Pass: pass, Diff: tc.Difficulty, Category: tc.Category, Score: score}
}

// evalAction calls LLM with ActionPrompt, then uses LLM-as-judge to score the response
func evalAction(llm *llmClient, tc testCase, prompt string) caseResult {
	reply, err := llm.chat(prompt, tc.TaskInput)
	if err != nil {
		return caseResult{TaskInput: tc.TaskInput, Expected: tc.ExpectedBehavior, Got: "[error: " + err.Error() + "]", Diff: tc.Difficulty, Category: tc.Category}
	}

	// Use LLM-as-judge
	judgeQuery := fmt.Sprintf(judgePrompt, tc.ExpectedBehavior, reply)
	judgeReply, err := llm.chat("You are a strict evaluator. Reply with only valid JSON.", judgeQuery)
	if err != nil {
		return caseResult{TaskInput: tc.TaskInput, Expected: tc.ExpectedBehavior, Got: reply, Diff: tc.Difficulty, Category: tc.Category}
	}

	// Extract JSON from judge reply (handle markdown code blocks)
	judgeReply = strings.TrimPrefix(judgeReply, "```json")
	judgeReply = strings.TrimPrefix(judgeReply, "```")
	judgeReply = strings.TrimSuffix(judgeReply, "```")
	judgeReply = strings.TrimSpace(judgeReply)

	var verdict struct {
		Pass  bool    `json:"pass"`
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(judgeReply), &verdict); err != nil {
		// Fallback: check if reply contains expected keywords
		return caseResult{TaskInput: tc.TaskInput, Expected: tc.ExpectedBehavior, Got: truncate(reply, 80), Diff: tc.Difficulty, Category: tc.Category}
	}

	return caseResult{
		TaskInput: tc.TaskInput,
		Expected:  tc.ExpectedBehavior,
		Got:       truncate(reply, 80),
		Pass:      verdict.Pass,
		Diff:      tc.Difficulty,
		Category:  tc.Category,
		Score:     verdict.Score,
	}
}

func loadGoldenDataset(path string) ([]testCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cases []testCase
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var tc testCase
		if err := json.Unmarshal([]byte(line), &tc); err != nil {
			return nil, fmt.Errorf("parse line: %w", err)
		}
		cases = append(cases, tc)
	}
	return cases, scanner.Err()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

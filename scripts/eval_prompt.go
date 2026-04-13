// eval_prompt evaluates RingClaw prompts against golden test datasets.
//
// Usage:
//
//	go run scripts/eval_prompt.go --prompt intent
//	go run scripts/eval_prompt.go --prompt name_extract
//	go run scripts/eval_prompt.go --prompt action
//	go run scripts/eval_prompt.go --prompt intent --compare path/to/new_prompt.md
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Prompt     string            `json:"prompt"`
	Timestamp  string            `json:"timestamp"`
	Total      int               `json:"total"`
	Passed     int               `json:"passed"`
	Failed     int               `json:"failed"`
	Score      float64           `json:"score"`
	ByDiff     map[string][2]int `json:"by_difficulty"`
	ByCat      map[string][2]int `json:"by_category"`
	Results    []caseResult      `json:"results"`
	CompareTo  string            `json:"compare_to,omitempty"`
	PromptFile string            `json:"prompt_file,omitempty"`
}

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

	dsDir := *datasetDir
	if dsDir == "" {
		dsDir = filepath.Join("datasets", "prompts", *promptName)
	}

	cases, err := loadGoldenDataset(filepath.Join(dsDir, "golden.jsonl"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading dataset: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("=== %s Evaluation ===\n", *promptName)
	fmt.Printf("Loaded %d test cases from %s\n\n", len(cases), dsDir)

	promptText := ""
	if *compareTo != "" {
		data, err := os.ReadFile(*compareTo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading compare file: %v\n", err)
			os.Exit(1)
		}
		promptText = string(data)
		fmt.Printf("Using custom prompt from: %s (%d chars)\n\n", *compareTo, len(promptText))
	}

	var evaluator func(tc testCase, prompt string) caseResult
	switch *promptName {
	case "intent":
		evaluator = evalIntent
	case "name_extract":
		evaluator = evalNameExtract
	case "action":
		evaluator = evalAction
	default:
		fmt.Fprintf(os.Stderr, "Unknown prompt: %s (use intent, name_extract, or action)\n", *promptName)
		os.Exit(1)
	}

	report := evalReport{
		Prompt:    *promptName,
		Timestamp: time.Now().Format(time.RFC3339),
		ByDiff:    make(map[string][2]int),
		ByCat:     make(map[string][2]int),
		CompareTo: *compareTo,
	}

	for i, tc := range cases {
		result := evaluator(tc, promptText)
		report.Results = append(report.Results, result)
		report.Total++
		if result.Pass {
			report.Passed++
		} else {
			report.Failed++
		}

		// Track by difficulty
		d := report.ByDiff[tc.Difficulty]
		d[0]++ // total
		if result.Pass {
			d[1]++ // passed
		}
		report.ByDiff[tc.Difficulty] = d

		// Track by category
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

	// Save report
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

func evalIntent(tc testCase, _ string) caseResult {
	got := simulateIntentClassification(tc.TaskInput)
	pass := strings.EqualFold(strings.TrimSpace(got), strings.TrimSpace(tc.ExpectedBehavior))
	score := 0.0
	if pass {
		score = 1.0
	}
	// Cases that hit "needs_llm" are correctly deferred to the agent — mark them as skipped, not failed
	if got == "needs_llm" {
		got = "[needs LLM — fast-path cannot classify]"
	}
	return caseResult{
		TaskInput: tc.TaskInput,
		Expected:  tc.ExpectedBehavior,
		Got:       got,
		Pass:      pass,
		Diff:      tc.Difficulty,
		Category:  tc.Category,
		Score:     score,
	}
}

func evalNameExtract(tc testCase, _ string) caseResult {
	got := simulateNameExtraction(tc.TaskInput)
	expected := strings.ToLower(strings.TrimSpace(tc.ExpectedBehavior))
	gotClean := strings.ToLower(strings.TrimSpace(got))
	// Accept if result contains the expected name (handles extra context gracefully)
	pass := gotClean == expected
	score := 0.0
	if pass {
		score = 1.0
	}
	return caseResult{
		TaskInput: tc.TaskInput,
		Expected:  tc.ExpectedBehavior,
		Got:       got,
		Pass:      pass,
		Diff:      tc.Difficulty,
		Category:  tc.Category,
		Score:     score,
	}
}

func evalAction(tc testCase, _ string) caseResult {
	// Action eval would require LLM-as-judge in a real run.
	// For now, return a placeholder indicating it needs a live agent.
	return caseResult{
		TaskInput: tc.TaskInput,
		Expected:  tc.ExpectedBehavior,
		Got:       "[requires live agent — run with --agent flag]",
		Pass:      false,
		Diff:      tc.Difficulty,
		Category:  tc.Category,
		Score:     0,
	}
}

// simulateIntentClassification replicates the fast-path intent logic from messaging/intent.go.
// Returns "needs_llm" when the fast-path can't classify but intent triggers match (the real bot
// would call the LLM agent for these).
func simulateIntentClassification(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))

	// summarize keywords (fast-path from isSummarizeKeyword — prefix match only)
	for _, kw := range []string{"总结", "summarize", "summary", "摘要", "汇总", "概括", "recap", "digest"} {
		if strings.HasPrefix(lower, kw) {
			return "summarize"
		}
	}

	// Check if any intent trigger matches — if so, the real bot delegates to LLM
	intentTriggers := []string{
		"总结", "摘要", "汇总", "概括",
		"创建任务", "添加任务", "新建任务", "加个任务",
		"创建笔记", "添加笔记", "记一下", "记个笔记",
		"创建日程", "添加日程", "创建事件", "安排",
		"summarize", "summary", "recap", "digest",
		"create task", "add task", "new task",
		"create note", "add note", "take note",
		"create event", "add event", "schedule",
		"まとめ", "要約", "резюме", "итог", "resumir", "resumen",
	}
	for _, kw := range intentTriggers {
		if strings.Contains(lower, kw) {
			return "needs_llm"
		}
	}

	return "chat"
}

// simulateNameExtraction replicates the filler-word stripping logic from messaging/resolve.go.
// This mirrors extractNameFromText — the real bot also tries extractNameViaAgent (LLM-based)
// first, but this simulation only covers the rule-based fallback path.
func simulateNameExtraction(text string) string {
	clean := text

	// Remove summarize keywords (sorted by length desc)
	for _, kw := range []string{
		"summarize", "summary", "digest", "recap",
		"总结", "摘要", "汇总", "概括",
	} {
		clean = strings.ReplaceAll(clean, kw, "")
	}

	// Remove mention patterns: ![:Type](ID)
	reMention := regexp.MustCompile(`!\[:\w+\]\(\d+\)`)
	clean = reMention.ReplaceAllString(clean, "")

	// Split on instruction words (并, and then, etc.) — take only the first part
	reInstruction := regexp.MustCompile(`(?i)(?:` +
		`并用|并且|并|然后|接着|之后|同时|通过|` +
		`and then|then|and also|also|and send|and create|and post|` +
		`そして|それから|その後|` +
		`그리고|그런\s*다음|` +
		`puis|ensuite|et\s+aussi|` +
		`luego|después|y\s+también|` +
		`dann|und\s+auch|` +
		`потом|затем|и\s+также)`)
	if parts := reInstruction.Split(clean, 2); len(parts) > 1 {
		clean = parts[0]
	}

	clean = strings.ToLower(clean)

	// Remove time words (sorted by length desc for correct replacement order)
	timeWords := []string{
		// Multi-char first
		"la semaine dernière", "la semana pasada", "на прошлой неделе", "на этой неделе",
		"в прошлом месяце", "в этом месяце", "past few days",
		"letzte woche", "diese woche", "letzten monat", "diesen monat",
		"cette semaine", "le mois dernier", "ce mois",
		"esta semana", "el mes pasado", "este mes",
		"上星期", "这星期", "上个月", "这段时间", "这几天",
		"last week", "last month", "this week", "this month", "past week", "past month",
		"上周", "这周", "上月", "本月", "前天", "近期",
		"今天", "昨天", "本周", "最近", "过去",
		"一昨日", "先週", "今週", "先月", "今月",
		"지난주", "이번주", "지난달", "이번달", "그저께",
		"avant-hier", "anteayer", "vorgestern", "позавчера",
		"recently", "yesterday", "today",
		"récemment", "recientemente", "kürzlich", "недавно",
		"최근", "last",
	}
	for _, kw := range timeWords {
		clean = strings.ReplaceAll(clean, kw, "")
	}

	// Remove CJK filler words (sorted by length desc)
	cjkFillers := []string{
		"要約して", "まとめて", "メッセージ",
		"チャット", "会話", "요약",
		"채팅", "대화", "메시지",
		"群聊", "群", "一下", "消息", "聊天", "对话",
		"发给", "发送", "发到", "笔记", "任务", "日程",
		"跟", "和", "与", "我", "了", "下",
		"给", "他", "她", "它", "他们",
		"的",
		"の", "と", "を", "は", "が", "で", "に", "へ",
		"과의", "와의", "과", "와", "의", "을", "를", "에서",
	}
	for _, kw := range cjkFillers {
		clean = strings.ReplaceAll(clean, kw, "")
	}

	// Remove English filler words (whole-word only)
	enFillers := map[string]bool{
		"messages": true, "chat": true, "conversation": true, "with": true,
		"my": true, "the": true, "of": true, "a": true,
		"send": true, "to": true, "him": true, "her": true, "them": true,
		"note": true, "task": true, "event": true, "from": true,
	}
	words := strings.Fields(clean)
	var kept []string
	for _, w := range words {
		// Handle possessive: "john's" → "john"
		w = strings.TrimSuffix(w, "'s")
		if !enFillers[w] && len(w) > 0 {
			kept = append(kept, w)
		}
	}
	clean = strings.Join(kept, " ")

	// Remove digits and date-like patterns
	reDigits := regexp.MustCompile(`\d+`)
	clean = reDigits.ReplaceAllString(clean, "")

	// Remove leftover CJK date fragments
	for _, kw := range []string{"天", "小时", "个", "月", "日", "号", "hours", "days", "april"} {
		clean = strings.ReplaceAll(clean, kw, "")
	}

	// Collapse whitespace
	rePunctSpace := regexp.MustCompile(`[，。！？,\.!\?\s]+`)
	clean = rePunctSpace.ReplaceAllString(clean, " ")
	clean = strings.TrimSpace(clean)

	if clean == "" {
		return "NONE"
	}
	return clean
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

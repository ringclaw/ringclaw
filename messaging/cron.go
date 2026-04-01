package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/robfig/cron/v3"
)

const cronTickInterval = 30 * time.Second

// CronScheduler runs cron jobs on schedule.
type CronScheduler struct {
	store        *CronStore
	client       *ringcentral.Client
	defaultChat  string
	getAgent     func(name string) agent.Agent
	cronParser   cron.Parser
	running      sync.Map // job ID -> struct{}, tracks in-flight jobs
}

// NewCronScheduler creates a scheduler.
func NewCronScheduler(store *CronStore, client *ringcentral.Client, defaultChat string, getAgent func(name string) agent.Agent) *CronScheduler {
	return &CronScheduler{
		store:       store,
		client:      client,
		defaultChat: defaultChat,
		getAgent:    getAgent,
		cronParser:  cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// Start runs the scheduler loop.
func (s *CronScheduler) Start(ctx context.Context) {
	slog.Info("cron scheduler started", "component", "cron")
	ticker := time.NewTicker(cronTickInterval)
	defer ticker.Stop()

	// Compute initial next-run times for jobs that don't have one
	s.initNextRuns()

	for {
		select {
		case <-ctx.Done():
			slog.Info("cron scheduler stopped", "component", "cron")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *CronScheduler) initNextRuns() {
	now := time.Now()
	for _, job := range s.store.List() {
		if !job.Enabled || !job.State.NextRunAt.IsZero() {
			continue
		}
		next, err := s.computeNextRun(job.Schedule, now)
		if err != nil {
			slog.Warn("cron: invalid schedule, disabling job", "component", "cron", "job", job.Name, "error", err)
			_ = s.store.SetEnabled(job.ID, false)
			continue
		}
		state := job.State
		state.NextRunAt = next
		s.store.UpdateState(job.ID, state)
	}
}

func (s *CronScheduler) tick(ctx context.Context) {
	now := time.Now()
	for _, job := range s.store.List() {
		if !job.Enabled {
			continue
		}
		if job.State.NextRunAt.IsZero() || now.Before(job.State.NextRunAt) {
			continue
		}
		// Skip if this job is already running
		if _, loaded := s.running.LoadOrStore(job.ID, struct{}{}); loaded {
			continue
		}
		// Advance NextRunAt immediately to prevent re-dispatch on next tick
		state := job.State
		next, err := s.computeNextRun(job.Schedule, now)
		if err == nil {
			state.NextRunAt = next
		} else {
			state.NextRunAt = now.Add(cronTickInterval * 2) // fallback: skip next tick
		}
		s.store.UpdateState(job.ID, state)
		go func(j CronJob) {
			defer s.running.Delete(j.ID)
			s.executeJob(ctx, j)
		}(job)
	}
}

func (s *CronScheduler) executeJob(ctx context.Context, job CronJob) {
	slog.Info("cron: executing job", "component", "cron", "name", job.Name, "id", job.ID)

	chatID := job.ChatID
	if chatID == "" {
		chatID = s.defaultChat
	}

	ag := s.getAgent(job.Agent)
	if ag == nil {
		slog.Warn("cron: no agent available, will retry", "component", "cron", "job", job.Name)
		// Reschedule for next tick instead of recording an error,
		// so one-shot (DeleteAfter) jobs are not permanently lost.
		state := job.State
		state.NextRunAt = time.Now().Add(cronTickInterval)
		s.store.UpdateState(job.ID, state)
		return
	}

	conversationID := fmt.Sprintf("cron:%s", job.ID)
	prompt := fmt.Sprintf("[Scheduled Task: %s]\n%s", job.Name, job.Message)

	reply, err := ag.Chat(ctx, conversationID, prompt)
	if err != nil {
		slog.Error("cron: agent error", "component", "cron", "job", job.Name, "error", err)
		s.recordResult(job, "error", err.Error())
		return
	}

	reply = strings.TrimSpace(reply)
	if reply != "" {
		text := fmt.Sprintf("**[Cron: %s]** %s", job.Name, reply)
		if err := SendTextReply(ctx, s.client, chatID, text); err != nil {
			slog.Error("cron: failed to send reply", "component", "cron", "job", job.Name, "error", err)
			s.recordResult(job, "error", "send failed: "+err.Error())
			return
		}
	}

	s.recordResult(job, "ok", "")
}

func (s *CronScheduler) recordResult(job CronJob, status, errMsg string) {
	state := job.State
	state.LastRunAt = time.Now()
	state.LastStatus = status
	state.RunCount++
	if status == "error" {
		state.ErrorCount++
		state.LastError = errMsg
	} else {
		state.LastError = ""
	}

	// Compute next run
	if state.DeleteAfter {
		// One-shot: disable after run
		_ = s.store.SetEnabled(job.ID, false)
	}

	next, err := s.computeNextRun(job.Schedule, time.Now())
	if err != nil {
		_ = s.store.SetEnabled(job.ID, false)
		slog.Warn("cron: cannot compute next run, disabling", "component", "cron", "job", job.Name, "error", err)
	} else {
		state.NextRunAt = next
	}

	s.store.UpdateState(job.ID, state)
}

// computeNextRun calculates the next run time for a schedule string.
func (s *CronScheduler) computeNextRun(schedule string, after time.Time) (time.Time, error) {
	schedule = strings.TrimSpace(schedule)

	// One-shot: "at:2026-04-01T09:00:00"
	if strings.HasPrefix(schedule, "at:") {
		t, err := time.Parse(time.RFC3339, strings.TrimPrefix(schedule, "at:"))
		if err != nil {
			// Try without timezone
			t, err = time.ParseInLocation("2006-01-02T15:04:05", strings.TrimPrefix(schedule, "at:"), time.Local)
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid at schedule: %w", err)
			}
		}
		if t.Before(after) {
			return time.Time{}, fmt.Errorf("at time %s is in the past", t)
		}
		return t, nil
	}

	// Interval: "every:5m"
	if strings.HasPrefix(schedule, "every:") {
		d, err := time.ParseDuration(strings.TrimPrefix(schedule, "every:"))
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid every schedule: %w", err)
		}
		if d <= 0 {
			return time.Time{}, fmt.Errorf("every duration must be positive")
		}
		return after.Add(d), nil
	}

	// Cron expression
	sched, err := s.cronParser.Parse(schedule)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression: %w", err)
	}
	return sched.Next(after), nil
}

// ComputeNextRun is exported for testing.
func (s *CronScheduler) ComputeNextRun(schedule string, after time.Time) (time.Time, error) {
	return s.computeNextRun(schedule, after)
}

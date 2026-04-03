//go:build windows

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func notifyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt)
}

// DETACHED_PROCESS prevents the child from inheriting the parent's console.
const detachedProcess = 0x00000008

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
}

func processExists(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), fmt.Sprintf("%d", pid))
}

func signalTerminate(p *os.Process) error {
	return p.Kill()
}

func killByName(exePath string) {
	myPID := os.Getpid()
	name := filepath.Base(exePath)
	// Use WMIC to terminate matching processes, excluding the current one.
	_ = exec.Command("wmic", "process", "where",
		fmt.Sprintf("Name='%s' AND ProcessId!=%d", name, myPID),
		"call", "terminate").Run()
}

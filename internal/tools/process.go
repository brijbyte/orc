package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/brijbyte/orc/internal/config"
)

// job is one managed background process.
type job struct {
	id      string
	cmd     string
	logPath string
	proc    *os.Process
	mu      sync.Mutex
	running bool
	exit    int
	signal  int
}

var (
	jobsMu sync.Mutex
	jobs   []*job
	nextID = 1
)

func findJob(id string) *job {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	for _, j := range jobs {
		if j.id == id {
			return j
		}
	}
	return nil
}

func (j *job) state() (running bool, exit, sig int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running, j.exit, j.signal
}

func processStart(cmd string) string {
	jobsMu.Lock()
	id := fmt.Sprintf("job-%d", nextID)
	nextID++
	jobsMu.Unlock()

	dir := config.Path("processes")
	os.MkdirAll(dir, 0o755)
	logPath := filepath.Join(dir, fmt.Sprintf("%d-%s.log", os.Getpid(), id))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "error: cannot create process log"
	}
	c := exec.Command("/bin/sh", "-c", cmd)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Stdout = logFile
	c.Stderr = logFile
	if err := c.Start(); err != nil {
		logFile.Close()
		return fmt.Sprintf("error: spawn failed: %v", err)
	}
	logFile.Close()

	j := &job{id: id, cmd: cmd, logPath: logPath, proc: c.Process, running: true}
	jobsMu.Lock()
	jobs = append(jobs, j)
	jobsMu.Unlock()
	go func() {
		err := c.Wait()
		j.mu.Lock()
		defer j.mu.Unlock()
		j.running = false
		if ee, ok := err.(*exec.ExitError); ok {
			ws := ee.Sys().(syscall.WaitStatus)
			if ws.Signaled() {
				j.signal = int(ws.Signal())
			} else {
				j.exit = ws.ExitStatus()
			}
		}
	}()
	return fmt.Sprintf("started %s\npid: %d\nlog: %s", id, c.Process.Pid, logPath)
}

func (j *job) statusLine() string {
	running, exit, sig := j.state()
	switch {
	case running:
		return fmt.Sprintf("%s running pid=%d", j.id, j.proc.Pid)
	case sig != 0:
		return fmt.Sprintf("%s stopped signal=%d", j.id, sig)
	default:
		return fmt.Sprintf("%s exited status=%d", j.id, exit)
	}
}

func processTool(a args) string {
	action := a.str("action")
	if action == "" {
		return "error: missing action"
	}
	if action == "list" {
		jobsMu.Lock()
		defer jobsMu.Unlock()
		if len(jobs) == 0 {
			return "no managed processes"
		}
		var out strings.Builder
		for _, j := range jobs {
			out.WriteString(j.statusLine() + "\n")
		}
		return out.String()
	}
	j := findJob(a.str("id"))
	if j == nil {
		return "error: process not found"
	}
	switch action {
	case "status":
		return fmt.Sprintf("%s\ncommand: %s\nlog: %s", j.statusLine(), j.cmd, j.logPath)
	case "logs":
		return jobLogs(j, a.num("offset", 0))
	case "stop":
		return stopJob(j)
	}
	return "error: unknown action"
}

func jobLogs(j *job, offset int) string {
	if offset < 0 {
		offset = 0
	}
	f, err := os.Open(j.logPath)
	if err != nil {
		return "error: cannot read process log"
	}
	defer f.Close()
	if _, err := f.Seek(int64(offset), 0); err != nil {
		return "error: invalid log offset"
	}
	buf := make([]byte, 20480)
	n, _ := f.Read(buf)
	return fmt.Sprintf("offset: %d\nnext_offset: %d\n%s", offset, offset+n, buf[:n])
}

func stopJob(j *job) string {
	if running, _, _ := j.state(); !running {
		return "already stopped"
	}
	syscall.Kill(-j.proc.Pid, syscall.SIGTERM)
	go func() {
		time.Sleep(2 * time.Second)
		if running, _, _ := j.state(); running {
			syscall.Kill(-j.proc.Pid, syscall.SIGKILL)
		}
	}()
	return "stopping"
}

// Cleanup stops all managed processes: SIGTERM, up to 1s grace, then SIGKILL.
func Cleanup() {
	jobsMu.Lock()
	all := append([]*job(nil), jobs...)
	jobsMu.Unlock()
	anyRunning := false
	for _, j := range all {
		if running, _, _ := j.state(); running {
			syscall.Kill(-j.proc.Pid, syscall.SIGTERM)
			anyRunning = true
		}
	}
	if !anyRunning {
		return
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		anyRunning = false
		for _, j := range all {
			if running, _, _ := j.state(); running {
				anyRunning = true
			}
		}
		if !anyRunning {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, j := range all {
		if running, _, _ := j.state(); running {
			syscall.Kill(-j.proc.Pid, syscall.SIGKILL)
		}
	}
}

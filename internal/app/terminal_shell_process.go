package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
)

// $SHELL names the login shell, not what actually runs: a bash that execs fish
// from its rc file leaves $SHELL at bash. A local terminal therefore reads the
// real program from its process, and keeps $SHELL's name only until it is
// known (or where processes cannot be read).

const (
	terminalShellFindEvery  = 150 * time.Millisecond
	terminalShellFindLimit  = 40
	terminalShellWatchEvery = 2 * time.Second
)

// terminalShellCandidates and terminalProcessName are seams for tests.
var (
	terminalShellCandidates = localTerminalShellCandidates
	terminalProcessName     = localTerminalProcessName
)

// terminalShellRegistry hands each new terminal the pty session leader it
// started. The terminal plugin does not expose its process, so a shell is
// recognised as the new session leader with a tty among this process's
// children. Claims are process-wide (every tab shares one process) and made
// in start order so concurrent starts cannot swap each other's shells.
var terminalShellRegistry struct {
	mu      sync.Mutex
	queue   []*terminalShellTracker
	claimed map[int]bool
}

type terminalShellTracker struct{ pid atomic.Int64 }

func (t *terminalShellTracker) enqueue() {
	r := &terminalShellRegistry
	r.mu.Lock()
	r.queue = append(r.queue, t)
	r.mu.Unlock()
}

// claim gives the oldest waiting terminal the lowest unclaimed candidate.
func (t *terminalShellTracker) claim(candidates []int) int {
	r := &terminalShellRegistry
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queue) == 0 || r.queue[0] != t {
		return 0
	}
	pick := 0
	for _, pid := range candidates {
		if !r.claimed[pid] && (pick == 0 || pid < pick) {
			pick = pid
		}
	}
	if pick == 0 {
		return 0
	}
	if r.claimed == nil {
		r.claimed = map[int]bool{}
	}
	r.claimed[pick] = true
	r.queue = r.queue[1:]
	t.pid.Store(int64(pick))
	return pick
}

// release forgets the terminal, whether or not it ever found its shell.
func (t *terminalShellTracker) release() {
	r := &terminalShellRegistry
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, queued := range r.queue {
		if queued == t {
			r.queue = append(r.queue[:i], r.queue[i+1:]...)
			break
		}
	}
	if pid := int(t.pid.Load()); pid != 0 {
		delete(r.claimed, pid)
	}
}

// trackTerminalShell keeps a local session's shell name equal to the program
// running as its shell, including after `exec fish` or a manual switch.
func (a *app) trackTerminalShell(sessionID, attempt, generation uint64, pane *terminal.Terminal) {
	if runtime.GOOS == "windows" || pane == nil {
		return
	}
	tracker := &terminalShellTracker{}
	tracker.enqueue()
	go func() {
		defer a.recoverBackground()
		defer tracker.release()
		pid, last := 0, ""
		for tick := 0; ; tick++ {
			wait := terminalShellWatchEvery
			if pid == 0 {
				wait = terminalShellFindEvery
			}
			select {
			case <-pane.Done():
				return
			case <-time.After(wait):
			}
			if pid == 0 {
				if tick >= terminalShellFindLimit {
					return
				}
				if pid = tracker.claim(terminalShellCandidates()); pid == 0 {
					continue
				}
			}
			name := terminalProcessName(pid)
			if name == "" || name == last {
				continue
			}
			last = name
			a.updateChanged(func() bool {
				_, current := a.terminalSessionByID(sessionID)
				if generation != a.generation || current == nil || attempt != current.attempt ||
					current.pane != pane || current.shell == name {
					return false
				}
				current.shell = name
				return a.terminalVisible()
			})
		}
	}()
}

// localTerminalShellCandidates lists this process's children that lead their
// own process group and own a terminal: the shells of the pty terminals.
func localTerminalShellCandidates() []int {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,pgid=,tty=").Output()
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil || ppid != self || pgid != pid {
			continue
		}
		if tty := fields[3]; tty == "?" || tty == "??" || tty == "-" {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// localTerminalProcessName is the program name of pid, without a login
// shell's leading "-", or "" once the process is gone.
func localTerminalProcessName(pid int) string {
	name := ""
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm"); err == nil {
			name = string(data)
		}
	}
	if name == "" {
		out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return ""
		}
		name = string(out)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.TrimPrefix(filepath.Base(name), "-")
}

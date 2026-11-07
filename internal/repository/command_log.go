package repository

import (
	"io"
	"strings"
	"sync"
	"time"
)

// commandLogLimit bounds how many commands the log keeps: the newest, so a
// long session stays readable and the memory it holds stays small.
const commandLogLimit = 400

// commandOutputLimit bounds the output kept for one command, so a command
// that streams pages of text cannot grow the log without limit.
const commandOutputLimit = 16 * 1024

// CommandEntry is one Git command the session ran: what it was, when, and
// what it printed. Failed marks a command whose error the app recorded.
type CommandEntry struct {
	At      time.Time
	Command string
	Output  string
	Failed  bool
}

// CommandLog is the record every command a session runs is written to, as
// Lazygit's command log shows them, so a user can see what the app did to the
// repository and why an action failed.
//
// It is written from the goroutine that runs Git and read from the one that
// builds the window, so its mutex guards every field.
type CommandLog struct {
	mu      sync.Mutex
	entries []CommandEntry
	// open is the entry the streamed output of the command now belongs to,
	// -1 before the first command.
	open int
	// pendingOutput holds an incomplete line. A URL or authorization header
	// may be split across arbitrary writes, so only complete lines are redacted
	// and published; the next command or Note flushes the final partial line.
	pendingOutput string
	// discardingOutputLine bounds an unterminated stream just as the output
	// limit bounds a complete one. Its text is omitted rather than publishing
	// an unredactable credential fragment.
	discardingOutputLine bool
}

func NewCommandLog() *CommandLog {
	return &CommandLog{open: -1}
}

// logCommand starts an entry for the command Git is about to run. It is the
// oscommands GUI IO's log hook.
func (l *CommandLog) logCommand(command string, _ bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushPendingOutputLocked()
	l.entries = append(l.entries, CommandEntry{At: time.Now(), Command: RedactSecrets(command)})
	if len(l.entries) > commandLogLimit {
		// Copy, so the log does not keep the trimmed prefix alive.
		l.entries = append([]CommandEntry(nil), l.entries[len(l.entries)-commandLogLimit:]...)
	}
	l.open = len(l.entries) - 1
}

// newWriter is the oscommands GUI IO's writer hook: the output of a streamed
// command lands in the entry of the command that is running.
func (l *CommandLog) newWriter() io.Writer {
	return commandWriter{log: l}
}

type commandWriter struct{ log *CommandLog }

func (w commandWriter) Write(p []byte) (int, error) {
	if w.log != nil {
		w.log.appendOutput(string(p))
	}
	return len(p), nil
}

func (l *CommandLog) appendOutput(text string) {
	if l == nil || text == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open < 0 || l.open >= len(l.entries) {
		return
	}
	if l.discardingOutputLine {
		newline := strings.Index(text, "\n")
		if newline < 0 {
			return
		}
		l.discardingOutputLine = false
		text = text[newline+1:]
	}
	l.pendingOutput += text
	lastNewline := strings.LastIndex(l.pendingOutput, "\n")
	if lastNewline < 0 {
		if len(l.pendingOutput) > commandOutputLimit {
			l.pendingOutput = ""
			l.discardingOutputLine = true
			l.appendOutputTextLocked("[unterminated output line omitted]\n")
		}
		return
	}
	complete := l.pendingOutput[:lastNewline+1]
	l.pendingOutput = l.pendingOutput[lastNewline+1:]
	l.appendOutputTextLocked(RedactSecrets(complete))
	if len(l.pendingOutput) > commandOutputLimit {
		l.pendingOutput = ""
		l.discardingOutputLine = true
		l.appendOutputTextLocked("[unterminated output line omitted]\n")
	}
}

// flushPendingOutputLocked publishes the unterminated final line. Holding it
// until this point is what prevents a split write from exposing half a secret.
func (l *CommandLog) flushPendingOutputLocked() {
	if l.pendingOutput == "" {
		return
	}
	text := RedactSecrets(l.pendingOutput)
	l.pendingOutput = ""
	l.discardingOutputLine = false
	l.appendOutputTextLocked(text)
}

func (l *CommandLog) appendOutputTextLocked(text string) {
	if l.open < 0 || l.open >= len(l.entries) {
		return
	}
	entry := &l.entries[l.open]
	if len(entry.Output) >= commandOutputLimit {
		return
	}
	room := commandOutputLimit - len(entry.Output)
	if len(text) > room {
		text = text[:room]
	}
	entry.Output += text
}

// Note marks the newest command as failed and adds text, usually the error
// Git printed, to its output. The app calls it when an action returns an
// error, so the command that failed and its message sit together even when
// Git did not stream the message.
func (l *CommandLog) Note(text string) {
	if l == nil {
		return
	}
	text = strings.TrimSpace(RedactSecrets(text))
	if text == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flushPendingOutputLocked()
	if len(l.entries) == 0 {
		l.entries = append(l.entries, CommandEntry{At: time.Now(), Command: "Git action"})
		l.open = 0
	}
	entry := &l.entries[len(l.entries)-1]
	entry.Failed = true
	if entry.Output != "" && !strings.HasSuffix(entry.Output, "\n") {
		entry.Output += "\n"
	}
	if len(entry.Output) < commandOutputLimit {
		room := commandOutputLimit - len(entry.Output)
		if len(text) > room {
			text = text[:room]
		}
		entry.Output += text
	}
}

// Entries returns the commands recorded, oldest first, as a copy the caller
// may keep while Git writes more.
func (l *CommandLog) Entries() []CommandEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]CommandEntry(nil), l.entries...)
}

// Clear drops every command recorded, as the command log's Clear button
// does.
func (l *CommandLog) Clear() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = nil
	l.open = -1
	l.pendingOutput = ""
	l.discardingOutputLine = false
}

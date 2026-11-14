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

// commandOutputLimit bounds how much output one command can keep. The same
// bound applies to an incomplete, not-yet-redacted line.
const commandOutputLimit = 16 * 1024

// CommandEntry is one Git or custom command and its output.
type CommandEntry struct {
	At      time.Time
	Command string
	Output  string
	Failed  bool
	// Origin distinguishes commands explicitly requested by the user from
	// Git commands started by this repository session.
	Origin CommandOrigin
	// Running reports commands accepted but not yet completed.
	Running bool
	// id binds streams and completions to this particular entry even when
	// newer commands start or older commands are trimmed.
	id uint64
}

// CommandOrigin is the source shown in the command-log sections.
type CommandOrigin uint8

const (
	// CommandOriginGit is a background Git or hosting operation.
	CommandOriginGit CommandOrigin = iota
	// CommandOriginUser is a one-off or configured command chosen by the user.
	CommandOriginUser
)

// commandLogStream owns an incomplete output line. Each writer has its own
// stream ID: one writer's output must never be reattributed merely because a
// custom or hosting command started while an ordinary Git command runs.
type commandLogStream struct {
	id                     uint64
	pending                string
	discardingUnterminated bool
}

// CommandLog records commands for one repository. Its mutex covers the
// logger itself; listener notifications run outside the lock and request UI
// redraws without blocking other commands or holding up Git.
type CommandLog struct {
	mu        sync.Mutex
	entries   []CommandEntry
	nextID    uint64
	listener  func()
	streams   map[uint64]*commandLogStream
	currentID uint64
	// registered identifies the exact callback emitted by the registered
	// runner, not every invocation with the same command text.
	registeredID      uint64
	registeredCommand string
	// expectedWriterID is the next writer's required owner. The registration
	// runner and writer factory share only this token, never a latest-command
	// assumption that another concurrent read can change mid-boundary.
	expectedWriterID uint64
	silentStream     bool
}

func NewCommandLog() *CommandLog {
	return &CommandLog{streams: make(map[uint64]*commandLogStream)}
}

// SetListener asks observer to refresh a currently visible log. Notifications
// are emitted for command starts and output changes, not merely on completion.
func (l *CommandLog) SetListener(listener func()) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.listener = listener
	l.mu.Unlock()
}

func (l *CommandLog) notifyListener() {
	if l == nil {
		return
	}
	l.mu.Lock()
	listener := l.listener
	l.mu.Unlock()
	if listener != nil {
		listener()
	}
}

func (l *CommandLog) appendEntryLocked(entry CommandEntry) uint64 {
	if entry.id == 0 {
		l.nextID++
		entry.id = l.nextID
	} else if entry.id > l.nextID {
		l.nextID = entry.id
	}
	l.entries = append(l.entries, entry)
	if len(l.entries) > commandLogLimit {
		l.entries = append([]CommandEntry(nil), l.entries[len(l.entries)-commandLogLimit:]...)
	}
	if l.streams == nil {
		l.streams = make(map[uint64]*commandLogStream)
	}
	return entry.id
}

// logCommand starts the entry for a Git command. When a runner already
// registered it (including at Run), ignore its duplicate library log callback.
func (l *CommandLog) logCommand(command string, _ bool) {
	if l == nil {
		return
	}
	command = RedactSecrets(command)
	l.mu.Lock()
	if l.registeredID != 0 && l.registeredCommand == command {
		// Only the nested callback for this registered invocation is
		// redundant. It is not a duplicate command, even if an ordinary
		// logged read has since advanced currentID.
		l.registeredID, l.registeredCommand = 0, ""
		l.mu.Unlock()
		return
	}
	l.currentID = l.appendEntryLocked(CommandEntry{At: time.Now(), Command: command})
	l.mu.Unlock()
	l.notifyListener()
}

// newWriter binds output to the current command. Both local and SSH runners
// register streamed commands explicitly before creating a writer, so this ID
// cannot be stolen by another command whose callback happens later.
func (l *CommandLog) newWriter() io.Writer {
	if l == nil {
		return io.Discard
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.currentID == 0 || l.silentStream {
		return io.Discard
	}
	id := l.currentID
	if l.expectedWriterID != 0 {
		id = l.expectedWriterID
		l.expectedWriterID = 0
	}
	return commandWriter{id: id, log: l}
}

type commandWriter struct {
	log *CommandLog
	id  uint64
}

func (w commandWriter) Write(p []byte) (int, error) {
	if w.log != nil && w.id != 0 {
		w.log.appendOutput(w.id, string(p))
	}
	return len(p), nil
}

func (l *CommandLog) streamLocked(id uint64) *commandLogStream {
	stream, ok := l.streams[id]
	if !ok {
		stream = &commandLogStream{id: id}
		if l.streams == nil {
			l.streams = make(map[uint64]*commandLogStream)
		}
		l.streams[id] = stream
	}
	return stream
}

func (l *CommandLog) appendOutput(id uint64, text string) {
	if l == nil || text == "" || id == 0 {
		return
	}
	changed := false
	l.mu.Lock()
	if l.entryIndexLocked(id) < 0 {
		// A later Clear or trim must never reattach an old writer's output to
		// a different command merely because the IDs are no longer active.
		l.mu.Unlock()
		return
	}
	stream := l.streamLocked(id)
	if stream.discardingUnterminated {
		newline := strings.Index(text, "\n")
		if newline < 0 {
			l.mu.Unlock()
			return
		}
		stream.discardingUnterminated = false
		text = text[newline+1:]
	}
	stream.pending += text
	lastNewline := strings.LastIndex(stream.pending, "\n")
	if lastNewline < 0 {
		if len(stream.pending) > commandOutputLimit {
			stream.pending = ""
			stream.discardingUnterminated = true
			changed = l.appendOutputTextLocked(id, "[unterminated output line omitted]\n")
		}
		l.mu.Unlock()
		if changed {
			l.notifyListener()
		}
		return
	}
	complete := stream.pending[:lastNewline+1]
	stream.pending = stream.pending[lastNewline+1:]
	changed = l.appendOutputTextLocked(id, RedactSecrets(complete))
	if len(stream.pending) > commandOutputLimit {
		stream.pending = ""
		stream.discardingUnterminated = true
		changed = l.appendOutputTextLocked(id, "[unterminated output line omitted]\n") || changed
	}
	if stream.pending == "" && !stream.discardingUnterminated {
		delete(l.streams, id)
	}
	l.mu.Unlock()
	if changed {
		l.notifyListener()
	}
}

func (l *CommandLog) entryIndexLocked(id uint64) int {
	for index := len(l.entries) - 1; index >= 0; index-- {
		if l.entries[index].id == id {
			return index
		}
	}
	return -1
}

func (l *CommandLog) appendOutputTextLocked(id uint64, text string) bool {
	index := l.entryIndexLocked(id)
	if index < 0 || text == "" {
		return false
	}
	entry := &l.entries[index]
	if len(entry.Output) >= commandOutputLimit {
		return false
	}
	room := commandOutputLimit - len(entry.Output)
	if len(text) > room {
		text = text[:room]
	}
	entry.Output += text
	return true
}

// flushPendingLocked publishes pending lines for Note's explicit
// action-failure boundary. Callers outside the log lock must use Note.
func (l *CommandLog) flushPendingLocked() {
	for id, stream := range l.streams {
		if stream.pending == "" {
			continue
		}
		l.appendOutputTextLocked(id, RedactSecrets(stream.pending))
		stream.pending = ""
		stream.discardingUnterminated = false
	}
	for id, stream := range l.streams {
		if stream.pending == "" && !stream.discardingUnterminated {
			delete(l.streams, id)
		}
	}
}

// Note marks the newest command failed and appends its error.
func (l *CommandLog) Note(text string) {
	if l == nil {
		return
	}
	text = strings.TrimSpace(RedactSecrets(text))
	if text == "" {
		return
	}
	l.mu.Lock()
	// A Note's explicit failure boundary retains the legacy behavior for
	// incomplete lines. Merely starting a different command must not flush
	// another writer's pending secret fragments, so that stays in its stream.
	l.flushPendingLocked()
	if len(l.entries) == 0 {
		l.appendEntryLocked(CommandEntry{At: time.Now(), Command: "Git action"})
	}
	entry := &l.entries[len(l.entries)-1]
	entry.Failed = true
	if entry.Output != "" && !strings.HasSuffix(entry.Output, "\n") {
		entry.Output += "\n"
	}
	if room := commandOutputLimit - len(entry.Output); len(text) > room {
		text = text[:room]
	}
	entry.Output += text
	l.mu.Unlock()
	l.notifyListener()
}

// Entries returns commands oldest first, as a copy the caller may retain.
func (l *CommandLog) Entries() []CommandEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]CommandEntry(nil), l.entries...)
}

// Clear drops all recorded commands and stream ownership without causing an
// already-running process to populate a later, different command's entry.
func (l *CommandLog) Clear() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.entries = nil
	l.currentID = 0
	l.registeredID = 0
	l.registeredCommand = ""
	l.expectedWriterID = 0
	l.silentStream = false
	l.streams = make(map[uint64]*commandLogStream)
	l.mu.Unlock()
	l.notifyListener()
}

// Begin records a background command immediately and returns its completion
// function. BeginOrigin distinguishes user-selected commands.
func (l *CommandLog) Begin(command string) func(output string, failed bool) {
	return l.BeginOrigin(command, CommandOriginGit)
}

// BeginUserCommand records a user command immediately as running.
func (l *CommandLog) BeginUserCommand(command string) func(output string, failed bool) {
	return l.BeginOrigin(command, CommandOriginUser)
}

// BeginOrigin records an accepted command before it executes.
func (l *CommandLog) BeginOrigin(command string, origin CommandOrigin) func(output string, failed bool) {
	if l == nil {
		return func(string, bool) {}
	}
	l.mu.Lock()
	id := l.appendEntryLocked(CommandEntry{
		At: time.Now(), Command: RedactSecrets(command), Origin: origin, Running: true,
	})
	l.mu.Unlock()
	l.notifyListener()
	return func(output string, failed bool) {
		l.mu.Lock()
		index := l.entryIndexLocked(id)
		if index >= 0 && l.entries[index].Running {
			entry := &l.entries[index]
			entry.Running = false
			entry.Failed = failed
			entry.Output = RedactSecrets(output)
			if len(entry.Output) > commandOutputLimit {
				entry.Output = entry.Output[:commandOutputLimit]
			}
		}
		l.mu.Unlock()
		l.notifyListener()
	}
}

// Record appends a completed ad-hoc command and its output. New work should
// use Begin before executing so pending commands are visible immediately.
func (l *CommandLog) Record(command, output string, failed bool) {
	l.Begin(command)(output, failed)
}

func (l *CommandLog) setSilentStream(silent bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.silentStream = silent
	l.mu.Unlock()
}

// registerStreamingCommand marks the actual CmdObj.Arg text entrusted to a
// runner. Unlike matching names globally, this must be a per-run registration:
// two identical pushes are different operations, not duplicates.
func (l *CommandLog) registerStreamingCommand(command string) {
	if l == nil {
		return
	}
	command = RedactSecrets(command)
	l.mu.Lock()
	// A fresh registration always gets a distinct log entry, even when its
	// command text matches the previous operation.
	next := l.appendEntryLocked(CommandEntry{At: time.Now(), Command: command})
	l.currentID = next
	l.expectedWriterID = next
	l.registeredID = next
	l.registeredCommand = command
	l.mu.Unlock()
}

// endStreamingCommand drops the registration token only after the runner has
// finished, including failures before writer creation. No later command can
// acquire that failed stream's output ID.
func (l *CommandLog) endStreamingCommand() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.expectedWriterID = 0
	l.silentStream = false
	l.mu.Unlock()
}

// Marker is a monotonically increasing command-log position. Capture one
// before an operation and another before its follow-up snapshots so failure
// reporting stays inside that operation rather than on whichever Git read
// happened later.
func (l *CommandLog) Marker() uint64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.nextID
}

// FailRange marks the matching command between start and end as failed, and
// attaches text to it. Commands after end, such as snapshots or ref refreshes,
// are deliberately excluded. If no command exists in that range, retain an
// informative Git-action entry instead of dropping the error entirely.
func (l *CommandLog) FailRange(start, end uint64, text string) {
	if l == nil {
		return
	}
	text = strings.TrimSpace(RedactSecrets(text))
	if text == "" {
		return
	}
	l.mu.Lock()
	index := -1
	for i := len(l.entries) - 1; i >= 0; i-- {
		entry := &l.entries[i]
		if entry.id <= start {
			break
		}
		if entry.id > end {
			continue
		}
		index = i
		break
	}
	if index < 0 {
		index = len(l.entries)
		l.appendEntryLocked(CommandEntry{At: time.Now(), Command: "Git action"})
	}
	entry := &l.entries[index]
	if stream := l.streams[entry.id]; stream != nil && stream.pending != "" {
		l.appendOutputTextLocked(entry.id, RedactSecrets(stream.pending))
		stream.pending = ""
		stream.discardingUnterminated = false
		delete(l.streams, entry.id)
	}
	entry.Failed = true
	if entry.Output != "" && !strings.HasSuffix(entry.Output, "\n") && len(entry.Output) < commandOutputLimit {
		entry.Output += "\n"
	}
	room := commandOutputLimit - len(entry.Output)
	if room > 0 {
		if len(text) > room {
			text = text[:room]
		}
		entry.Output += text
	}
	l.mu.Unlock()
	l.notifyListener()
}

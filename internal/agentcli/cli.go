package agentcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"lazygitapp/internal/agentwork"
)

const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Execute runs automation args after the app's "agent" prefix. It never exits
// the process itself: the desktop entry point routes its result to os.Exit.
func Execute(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return ExitUsage
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printUsage(stdout)
		return ExitOK
	}
	if args[0] == "rpc" {
		fs := flag.NewFlagSet("agent rpc", flag.ContinueOnError)
		fs.SetOutput(stderr)
		dataDir := fs.String("data-dir", "", "TanGit user data directory (or "+DataDirEnvironment+")")
		if code := parseFlags(fs, args[1:], stderr); code >= 0 {
			return code
		}
		service, err := NewService(*dataDir)
		if err != nil {
			return commandError(stderr, "rpc", err)
		}
		if err := service.ServeRPC(ctx, stdin, stdout); err != nil {
			return commandError(stderr, "rpc", err)
		}
		return ExitOK
	}

	command := args[0]
	fs := flag.NewFlagSet("agent "+command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data-dir", "", "TanGit user data directory (or "+DataDirEnvironment+")")
	jsonOutput := fs.Bool("json", false, "write machine-readable JSON")

	switch command {
	case "list":
		repositoryID := fs.String("repository", "", "exact repository ID filter")
		if code := parseFlags(fs, args[1:], stderr); code >= 0 {
			return code
		}
		service, err := NewService(*dataDir)
		if err != nil {
			return commandError(stderr, command, err)
		}
		tasks, err := service.List(ctx, *repositoryID)
		if err != nil {
			return commandError(stderr, command, err)
		}
		if *jsonOutput {
			return writeCLIResult(stdout, stderr, command, tasks)
		}
		if len(tasks) == 0 {
			fmt.Fprintln(stdout, "No agent tasks.")
		}
		for _, task := range tasks {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", task.ID, task.Status, task.RepositoryID, task.Title)
		}
		return ExitOK

	case "show":
		taskID := fs.String("id", "", "explicit agent task ID")
		if code := parseFlags(fs, args[1:], stderr); code >= 0 {
			return code
		}
		service, err := NewService(*dataDir)
		if err != nil {
			return commandError(stderr, command, err)
		}
		task, err := service.Show(ctx, *taskID)
		if err != nil {
			return commandError(stderr, command, err)
		}
		if *jsonOutput {
			return writeCLIResult(stdout, stderr, command, task)
		}
		printTask(stdout, task)
		return ExitOK

	case "diff":
		taskID := fs.String("id", "", "explicit agent task ID")
		file := fs.String("file", "", "one final file path")
		ignoreWhitespace := fs.Bool("ignore-whitespace", false, "ignore whitespace when comparing")
		contextLines := fs.Int("context", 3, "diff context lines")
		if code := parseFlags(fs, args[1:], stderr); code >= 0 {
			return code
		}
		if *contextLines < 0 {
			return commandError(stderr, command, errors.New("context lines cannot be negative"))
		}
		service, err := NewService(*dataDir)
		if err != nil {
			return commandError(stderr, command, err)
		}
		result, err := service.Diff(ctx, DiffOptions{
			TaskID: *taskID, File: *file, IgnoreWhitespace: *ignoreWhitespace, ContextLines: *contextLines,
		})
		if err != nil {
			return commandError(stderr, command, err)
		}
		if *jsonOutput {
			return writeCLIResult(stdout, stderr, command, result)
		}
		if *file != "" {
			if result.Patch != "" {
				fmt.Fprint(stdout, result.Patch)
			} else if result.NewText != "" {
				fmt.Fprint(stdout, result.NewText)
			}
			return ExitOK
		}
		fmt.Fprintf(stdout, "Task: %s\nBase: %s\n", result.TaskID, result.BaseCommit)
		for _, file := range result.Files {
			previous := ""
			if file.PreviousPath != "" && file.PreviousPath != file.Path {
				previous = file.PreviousPath + " -> "
			}
			fmt.Fprintf(stdout, "%s\t%s%s\t+%d -%d\n", file.Status, previous, file.Path, file.LinesAdded, file.LinesDeleted)
		}
		return ExitOK

	case "create":
		params := CreateParams{}
		fs.StringVar(&params.Repository, "repository", "", "source repository path (required)")
		fs.StringVar(&params.Path, "path", "", "new worktree path (required)")
		fs.StringVar(&params.Branch, "branch", "", "new branch name (required)")
		fs.StringVar(&params.Base, "base", "", "base ref to freeze (required)")
		fs.StringVar(&params.Title, "title", "", "task title (required)")
		fs.StringVar(&params.Request, "request", "", "agent request text (required)")
		fs.StringVar(&params.Profile.Name, "profile", "", "command profile name")
		fs.StringVar(&params.Profile.LaunchCommand, "launch-command", "", "agent launch command template")
		fs.StringVar(&params.Profile.ResumeCommand, "resume-command", "", "agent resume command template")
		fs.StringVar(&params.Profile.SetupCommand, "setup-command", "", "worktree setup command")
		fs.StringVar(&params.Profile.TestCommand, "test-command", "", "test command template")
		environment := environmentFlags{}
		fs.Var(&environment, "env", "profile environment KEY=VALUE (repeatable)")
		fs.BoolVar(&params.Yes, "yes", false, "confirm Git worktree and branch creation")
		if code := parseFlags(fs, args[1:], stderr); code >= 0 {
			return code
		}
		params.Profile.Environment = environment.values
		service, err := NewService(*dataDir)
		if err != nil {
			return commandError(stderr, command, err)
		}
		created, err := service.Create(ctx, params)
		if err != nil {
			return commandError(stderr, command, err)
		}
		if *jsonOutput {
			return writeCLIResult(stdout, stderr, command, created)
		}
		printTask(stdout, created)
		return ExitOK

	case "report":
		params := ReportParams{}
		fs.StringVar(&params.TaskID, "id", "", "explicit agent task ID")
		fs.StringVar(&params.ExpectedUpdatedAt, "expected-updated-at", "", "task updatedAt revision from show/list")
		var sessionID, summary optionalString
		fs.Var(&sessionID, "session-id", "agent session ID")
		fs.Var(&summary, "summary", "agent completion summary")
		var status string
		fs.StringVar(&status, "status", "", "needs_input, ready_for_review, blocked, or failed")
		if code := parseFlags(fs, args[1:], stderr); code >= 0 {
			return code
		}
		params.SessionID, params.Summary = sessionID.pointer(), summary.pointer()
		params.Status = agentwork.Status(status)
		return runTaskMutation(ctx, stdout, stderr, *dataDir, *jsonOutput, command, func(service *Service) (agentwork.Task, error) {
			return service.Report(ctx, params)
		})

	case "comment":
		params := CommentParams{}
		fs.StringVar(&params.TaskID, "id", "", "explicit agent task ID")
		fs.StringVar(&params.ExpectedUpdatedAt, "expected-updated-at", "", "task updatedAt revision from show/list")
		fs.StringVar(&params.Text, "text", "", "feedback/request text to append")
		if code := parseFlags(fs, args[1:], stderr); code >= 0 {
			return code
		}
		return runTaskMutation(ctx, stdout, stderr, *dataDir, *jsonOutput, command, func(service *Service) (agentwork.Task, error) {
			return service.Comment(ctx, params)
		})

	case "test-result":
		params := TestResultParams{}
		fs.StringVar(&params.TaskID, "id", "", "explicit agent task ID")
		fs.StringVar(&params.ExpectedUpdatedAt, "expected-updated-at", "", "task updatedAt revision from show/list")
		var status, commandText, started, finished string
		fs.StringVar(&status, "status", "", "passed or failed")
		fs.StringVar(&commandText, "command", "", "test command that ran")
		fs.StringVar(&started, "started-at", "", "RFC3339 start timestamp")
		fs.StringVar(&finished, "finished-at", "", "RFC3339 finish timestamp")
		exitCode := fs.Int("exit-code", 0, "numeric process exit code (required)")
		if code := parseFlags(fs, args[1:], stderr); code >= 0 {
			return code
		}
		if !flagWasSet(fs, "exit-code") {
			return commandError(stderr, command, errors.New("--exit-code is required"))
		}
		var err error
		params.StartedAt, err = parseOptionalTime(started, "--started-at")
		if err != nil {
			return commandError(stderr, command, err)
		}
		params.FinishedAt, err = parseOptionalTime(finished, "--finished-at")
		if err != nil {
			return commandError(stderr, command, err)
		}
		params.Status, params.Command, params.ExitCode = agentwork.TestStatus(status), commandText, *exitCode
		return runTaskMutation(ctx, stdout, stderr, *dataDir, *jsonOutput, command, func(service *Service) (agentwork.Task, error) {
			return service.RecordTestResult(ctx, params)
		})

	case "patch", "preview-patch", "apply-patch":
		params := PatchParams{}
		fs.StringVar(&params.Destination, "destination", "", "explicit destination worktree")
		fs.StringVar(&params.Patch, "patch", "", "patch file to inspect")
		fs.BoolVar(&params.Yes, "yes", false, "confirm applying a clean patch")
		if code := parseFlags(fs, args[1:], stderr); code >= 0 {
			return code
		}
		service, err := NewService(*dataDir)
		if err != nil {
			return commandError(stderr, command, err)
		}
		var result PatchResult
		if command == "apply-patch" || (command == "patch" && params.Yes) {
			result, err = service.ApplyPatch(ctx, params)
		} else {
			if command == "apply-patch" {
				params.Yes = false
			}
			result, err = service.PreviewPatch(ctx, params)
		}
		if err != nil {
			return commandError(stderr, command, err)
		}
		if *jsonOutput {
			return writeCLIResult(stdout, stderr, command, result)
		}
		printPatch(stdout, result)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "agent: unknown command %q\n", command)
		printUsage(stderr)
		return ExitUsage
	}
}

func parseFlags(fs *flag.FlagSet, args []string, stderr io.Writer) int {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "%s: unexpected positional arguments: %s\n", fs.Name(), strings.Join(fs.Args(), " "))
		return ExitUsage
	}
	return -1
}

func commandError(stderr io.Writer, command string, err error) int {
	fmt.Fprintf(stderr, "agent %s: %v\n", command, err)
	return ExitError
}

func writeCLIResult(stdout, stderr io.Writer, command string, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return commandError(stderr, command, fmt.Errorf("write JSON output: %w", err))
	}
	return ExitOK
}

func runTaskMutation(ctx context.Context, stdout, stderr io.Writer, dataDir string, jsonOutput bool, command string, mutate func(*Service) (agentwork.Task, error)) int {
	service, err := NewService(dataDir)
	if err != nil {
		return commandError(stderr, command, err)
	}
	updated, err := mutate(service)
	if err != nil {
		return commandError(stderr, command, err)
	}
	if jsonOutput {
		return writeCLIResult(stdout, stderr, command, updated)
	}
	printTask(stdout, updated)
	return ExitOK
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `TanGit local agent automation:
  tangit agent list [--repository ID] [--json]
  tangit agent show --id TASK [--json]
  tangit agent diff --id TASK [--file PATH] [--json]
  tangit agent create --repository PATH --path PATH --branch NAME --base REF --title TEXT --request TEXT --yes
  tangit agent report --id TASK --expected-updated-at TIME [--session-id TEXT] [--summary TEXT] [--status STATE]
  tangit agent comment --id TASK --expected-updated-at TIME --text TEXT
  tangit agent test-result --id TASK --expected-updated-at TIME --status passed|failed --command CMD --exit-code N
  tangit agent patch --destination WORKTREE --patch FILE [--yes]
  tangit agent preview-patch --destination WORKTREE --patch FILE
  tangit agent apply-patch --destination WORKTREE --patch FILE --yes
  tangit agent rpc

All commands accept --data-dir DIR; TANGIT_AGENT_DATA_DIR is the environment fallback.`)
}

func printTask(writer io.Writer, task agentwork.Task) {
	fmt.Fprintf(writer, "ID: %s\nRepository: %s\nTitle: %s\nStatus: %s\nBranch: %s\nWorktree: %s\nBase: %s (%s)\nUpdated: %s\n",
		task.ID, task.RepositoryID, task.Title, task.Status, task.Branch, task.WorktreePath, task.BaseRef, task.BaseCommit, task.UpdatedAt.Format(time.RFC3339Nano))
	if task.SessionID != "" {
		fmt.Fprintf(writer, "Session: %s\n", task.SessionID)
	}
	if task.Summary != "" {
		fmt.Fprintf(writer, "Summary: %s\n", task.Summary)
	}
	fmt.Fprintf(writer, "Request:\n%s\n", task.Request)
	if task.LastTest != nil {
		result := task.LastTest
		exit := "unset"
		if result.ExitCode != nil {
			exit = fmt.Sprint(*result.ExitCode)
		}
		fmt.Fprintf(writer, "Last test: %s (exit %s)\nCommand: %s\nStarted: %s\nFinished: %s\n",
			result.Status, exit, result.Command, result.StartedAt.Format(time.RFC3339Nano), result.FinishedAt.Format(time.RFC3339Nano))
	}
	fmt.Fprintf(writer, "Checkpoints: %d\n", len(task.Checkpoints))
	for _, checkpoint := range task.Checkpoints {
		fmt.Fprintf(writer, "  %s\t%s\t%s\t%s\n", checkpoint.ID, checkpoint.HeadCommit, checkpoint.Reason, checkpoint.CreatedAt.Format(time.RFC3339Nano))
	}
}

func printPatch(writer io.Writer, result PatchResult) {
	fmt.Fprintf(writer, "Destination: %s\nPatch: %s\nClean: %t\nApplied: %t\nFiles: %d (+%d -%d)\n",
		result.Destination, result.Path, result.Clean, result.Applied, len(result.Files), result.Additions, result.Deletions)
	if result.Diagnostic != "" {
		fmt.Fprintf(writer, "Diagnostic: %s\n", result.Diagnostic)
	}
	for _, file := range result.Files {
		fmt.Fprintf(writer, "  %s\t+%d -%d\n", file.Path, file.Additions, file.Deletions)
	}
	if result.TextTruncated {
		fmt.Fprintln(writer, "Preview text is truncated; the source file remains authoritative.")
	}
	if result.Text != "" {
		fmt.Fprintf(writer, "\n%s", result.Text)
	}
}

type optionalString struct {
	set   bool
	value string
}

func (value *optionalString) String() string { return value.value }
func (value *optionalString) Set(text string) error {
	value.set, value.value = true, text
	return nil
}
func (value optionalString) pointer() *string {
	if !value.set {
		return nil
	}
	return &value.value
}

type environmentFlags struct {
	values map[string]string
}

func (flags *environmentFlags) String() string { return "" }
func (flags *environmentFlags) Set(value string) error {
	key, item, found := strings.Cut(value, "=")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return errors.New("environment must use KEY=VALUE")
	}
	if flags.values == nil {
		flags.values = map[string]string{}
	}
	flags.values[key] = item
	return nil
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(flag *flag.Flag) {
		if flag.Name == name {
			found = true
		}
	})
	return found
}

func parseOptionalTime(value, name string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s as RFC3339: %w", name, err)
	}
	return parsed.UTC(), nil
}

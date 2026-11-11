package repository

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BlameLine is one source line and the commit that last changed it at HEAD.
// Date preserves the author's calendar day in the timezone recorded by Git.
type BlameLine struct {
	Hash string
	// CommitPath and CommitLine locate this source in its blamed revision,
	// which can differ from the working tree after renames and later edits.
	CommitPath    string
	CommitLine    int
	Author        string
	AuthorEmail   string
	Date          string
	UnixTimestamp int64
	Subject       string
	Current       bool
	Text          string
}

// BlameFile reads HEAD rather than sizing the request from a modified working
// tree. Porcelain output keeps names, tabs, whitespace, and unusual source
// lines unambiguous instead of trying to parse Git's display-oriented columns.
func (s *gitSession) BlameFile(ctx context.Context, file File) ([]BlameLine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !file.Tracked || file.Deleted {
		return nil, fmt.Errorf("blame requires a tracked file present at HEAD")
	}
	lines, err := s.blameFile(ctx, file.Path, true)
	if err != nil {
		return nil, err
	}
	return lines, nil
}

// BlameWorkingTreeFile attributes the saved working-tree lines to their last
// commits. New or changed lines retain Git's all-zero, uncommitted identity.
func (s *gitSession) BlameWorkingTreeFile(ctx context.Context, path string) ([]BlameLine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lines, err := s.blameFile(ctx, path, false)
	if err == nil {
		// A matching configured email is Git's own identity, displayed as
		// "You" in the active-line annotation. Missing config is harmless.
		if output, configErr := s.cmd.New([]string{"git", "config", "user.email"}).DontLog().RunWithOutput(); configErr == nil {
			email := strings.TrimSpace(output)
			for index := range lines {
				lines[index].Current = email != "" && strings.EqualFold(lines[index].AuthorEmail, email)
			}
		}
		return lines, nil
	}
	// Git refuses a path absent from HEAD, including every path in an unborn
	// repository. Those saved lines are new work, not an unusable blame view.
	if _, headErr := s.cmd.New([]string{"git", "cat-file", "-e", "HEAD:" + path}).DontLog().RunWithOutput(); headErr == nil {
		return nil, err
	}
	filename, pathErr := s.worktreeFilePath(s.git.RepoPaths.WorktreePath(), filepath.FromSlash(path))
	if pathErr != nil {
		return nil, pathErr
	}
	content, readErr := s.fileService().ReadFile(filename)
	if readErr != nil {
		return nil, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	textLines := strings.Split(text, "\n")
	uncommitted := make([]BlameLine, 0, len(textLines))
	for _, line := range textLines {
		uncommitted = append(uncommitted, BlameLine{
			Hash: strings.Repeat("0", 40), Author: "Not Committed Yet", Text: line,
		})
	}
	return uncommitted, nil
}

func (s *gitSession) blameFile(ctx context.Context, path string, head bool) ([]BlameLine, error) {
	args := []string{"git", "blame", "--line-porcelain"}
	if head {
		args = append(args, "HEAD")
	}
	args = append(args, "--", path)
	output, err := s.cmd.New(args).DontLog().RunWithOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("blame file: %w", err)
	}
	lines, err := parseBlamePorcelain(output)
	if err != nil {
		return nil, fmt.Errorf("blame file: %w", err)
	}
	return lines, nil
}

type blameCommit struct {
	path        string
	author      string
	authorEmail string
	date        string
	unix        int64
	subject     string
}

func parseBlamePorcelain(output string) ([]BlameLine, error) {
	commits := map[string]blameCommit{}
	var result []BlameLine
	remaining := strings.Split(output, "\n")
	for index := 0; index < len(remaining); index++ {
		fields := strings.Fields(remaining[index])
		if len(fields) < 3 {
			if strings.TrimSpace(remaining[index]) == "" {
				continue
			}
			return nil, fmt.Errorf("malformed blame header %q", remaining[index])
		}
		hash := strings.TrimPrefix(fields[0], "^")
		if len(hash) != 40 && len(hash) != 64 {
			return nil, fmt.Errorf("malformed blame hash %q", fields[0])
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return nil, fmt.Errorf("malformed blame hash %q", fields[0])
		}
		sourceLine, err := strconv.Atoi(fields[1])
		if err != nil || sourceLine < 1 {
			return nil, fmt.Errorf("malformed blame source line %q", fields[1])
		}
		if _, err := strconv.Atoi(fields[2]); err != nil {
			return nil, fmt.Errorf("malformed blame result line %q", fields[2])
		}

		commit := commits[hash]
		var unix int64
		timezone := ""
		content := ""
		foundContent := false
		for index++; index < len(remaining); index++ {
			line := remaining[index]
			if strings.HasPrefix(line, "\t") {
				content = strings.TrimPrefix(line, "\t")
				foundContent = true
				break
			}
			key, value, _ := strings.Cut(line, " ")
			switch key {
			case "author":
				commit.author = value
			case "author-mail":
				commit.authorEmail = strings.Trim(value, "<>")
			case "author-time":
				parsed, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("malformed blame author time %q", value)
				}
				unix = parsed
			case "author-tz":
				timezone = value
			case "summary":
				commit.subject = value
			case "filename":
				commit.path = value
			}
		}
		if !foundContent {
			return nil, fmt.Errorf("blame for %s has no source line", hash)
		}
		commit.unix = unix
		if timezone != "" {
			commit.date = blameAuthorDate(unix, timezone)
		}
		commits[hash] = commit
		result = append(result, BlameLine{
			Hash: hash, CommitPath: commit.path, CommitLine: sourceLine,
			Author: commit.author, AuthorEmail: commit.authorEmail, Date: commit.date,
			UnixTimestamp: commit.unix, Subject: commit.subject, Text: content,
		})
	}
	return result, nil
}

func blameAuthorDate(unix int64, timezone string) string {
	zone, err := time.Parse("-0700", timezone)
	if err != nil {
		return time.Unix(unix, 0).UTC().Format("2006-01-02")
	}
	_, offset := zone.Zone()
	return time.Unix(unix, 0).In(time.FixedZone("", offset)).Format("2006-01-02")
}

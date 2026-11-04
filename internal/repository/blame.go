package repository

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// BlameLine is one source line and the commit that last changed it at HEAD.
// Date preserves the author's calendar day in the timezone recorded by Git.
type BlameLine struct {
	Hash   string
	Author string
	Date   string
	Text   string
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
	output, err := s.cmd.New([]string{"git", "blame", "--line-porcelain", "HEAD", "--", file.Path}).DontLog().RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("blame file: %w", err)
	}
	lines, err := parseBlamePorcelain(output)
	if err != nil {
		return nil, fmt.Errorf("blame file: %w", err)
	}
	return lines, nil
}

type blameCommit struct {
	author string
	date   string
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
		if _, err := strconv.Atoi(fields[1]); err != nil {
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
			case "author-time":
				parsed, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("malformed blame author time %q", value)
				}
				unix = parsed
			case "author-tz":
				timezone = value
			}
		}
		if !foundContent {
			return nil, fmt.Errorf("blame for %s has no source line", hash)
		}
		if timezone != "" {
			commit.date = blameAuthorDate(unix, timezone)
		}
		commits[hash] = commit
		result = append(result, BlameLine{Hash: hash, Author: commit.author, Date: commit.date, Text: content})
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

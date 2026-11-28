package forge

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type MutationKind string

const (
	MutationComment     MutationKind = "comment"
	MutationReply       MutationKind = "reply"
	MutationLineComment MutationKind = "line-comment"
	MutationResolve     MutationKind = "resolve"
	MutationApprove     MutationKind = "approve"
)

// Mutation describes one review-side effect without exposing provider command
// syntax. Discussion is required for replies and thread resolution.
type Mutation struct {
	Kind       MutationKind
	Body       string
	Discussion Discussion
	Resolved   bool
	Line       LineTarget
	HeadSHA    string
}

type LineTarget struct {
	Path         string
	PreviousPath string
	OldLine      int
	NewLine      int
	BaseSHA      string
	StartSHA     string
	HeadSHA      string
}

func (c *cliClient) Mutate(ctx context.Context, request ChangeRequest, mutation Mutation) error {
	if c == nil || c.runner == nil {
		return errors.New("forge command runner is unavailable")
	}
	if request.Number <= 0 {
		return errors.New("change request number is required")
	}
	if mutation.Kind == MutationComment || mutation.Kind == MutationReply || mutation.Kind == MutationLineComment {
		mutation.Body = strings.TrimSpace(mutation.Body)
		if mutation.Body == "" {
			return errors.New("comment body is required")
		}
	}
	tool, err := c.runner.PullRequestTool(ctx)
	if err != nil {
		return err
	}
	if (tool == "gh" && request.Provider != GitHub) || (tool == "glab" && request.Provider != GitLab) {
		return errors.New("change request provider does not match repository")
	}
	var args []string
	switch tool {
	case "gh":
		args, err = githubMutationArgs(request, mutation)
	case "glab":
		args, err = gitlabMutationArgs(request, mutation)
	default:
		err = fmt.Errorf("unsupported hosting tool %q", tool)
	}
	if err != nil {
		return err
	}
	_, err = c.runner.RunHostingCommand(ctx, tool, args)
	return err
}

func githubMutationArgs(request ChangeRequest, mutation Mutation) ([]string, error) {
	base := fmt.Sprintf("repos/{owner}/{repo}/pulls/%d", request.Number)
	switch mutation.Kind {
	case MutationComment:
		return writeAPIArgs(fmt.Sprintf("repos/{owner}/{repo}/issues/%d/comments", request.Number), "body", mutation.Body), nil
	case MutationReply:
		if mutation.Discussion.Kind != DiscussionCode {
			return writeAPIArgs(fmt.Sprintf("repos/{owner}/{repo}/issues/%d/comments", request.Number), "body", mutation.Body), nil
		}
		replyID := mutation.Discussion.ReplyID
		if replyID == "" {
			replyID = mutation.Discussion.ID
		}
		commentID, err := strconv.ParseInt(replyID, 10, 64)
		if err != nil || commentID <= 0 {
			return nil, errors.New("GitHub review comment ID is invalid")
		}
		return writeAPIArgs(fmt.Sprintf("%s/comments/%d/replies", base, commentID), "body", mutation.Body), nil
	case MutationApprove:
		fields := [][2]string{{"event", "APPROVE"}}
		if mutation.HeadSHA != "" {
			fields = append(fields, [2]string{"commit_id", mutation.HeadSHA})
		}
		return writeAPIFields("POST", base+"/reviews", fields), nil
	case MutationLineComment:
		if mutation.Line.Path == "" || mutation.Line.HeadSHA == "" || (mutation.Line.OldLine <= 0 && mutation.Line.NewLine <= 0) {
			return nil, errors.New("GitHub line comment position is incomplete")
		}
		line, side := mutation.Line.NewLine, "RIGHT"
		if line <= 0 {
			line, side = mutation.Line.OldLine, "LEFT"
		}
		return writeAPIFields("POST", base+"/comments", [][2]string{
			{"body", mutation.Body}, {"commit_id", mutation.Line.HeadSHA}, {"path", mutation.Line.Path},
			{"line", strconv.Itoa(line)}, {"side", side},
		}), nil
	case MutationResolve:
		if mutation.Discussion.Kind != DiscussionCode || mutation.Discussion.ID == "" || !mutation.Discussion.Resolvable {
			return nil, errors.New("GitHub discussion is not resolvable")
		}
		operation := "resolveReviewThread"
		if !mutation.Resolved {
			operation = "unresolveReviewThread"
		}
		query := fmt.Sprintf("mutation($threadId:ID!){%s(input:{threadId:$threadId}){thread{id isResolved}}}", operation)
		return []string{"api", "graphql", "--raw-field", "query=" + query, "--raw-field", "threadId=" + mutation.Discussion.ID}, nil
	default:
		return nil, fmt.Errorf("unsupported mutation %q", mutation.Kind)
	}
}

func gitlabMutationArgs(request ChangeRequest, mutation Mutation) ([]string, error) {
	base := fmt.Sprintf("projects/:fullpath/merge_requests/%d", request.Number)
	switch mutation.Kind {
	case MutationComment:
		return writeAPIArgs(base+"/notes", "body", mutation.Body), nil
	case MutationReply:
		if mutation.Discussion.ID == "" {
			return nil, errors.New("GitLab discussion ID is required")
		}
		return writeAPIArgs(base+"/discussions/"+url.PathEscape(mutation.Discussion.ID)+"/notes", "body", mutation.Body), nil
	case MutationResolve:
		if mutation.Discussion.ID == "" || !mutation.Discussion.Resolvable {
			return nil, errors.New("GitLab discussion is not resolvable")
		}
		return writeAPIArgsMethod("PUT", base+"/discussions/"+url.PathEscape(mutation.Discussion.ID), "resolved", strconv.FormatBool(mutation.Resolved)), nil
	case MutationApprove:
		if mutation.HeadSHA != "" {
			return writeAPIArgs(base+"/approve", "sha", mutation.HeadSHA), nil
		}
		return []string{"api", "--method", "POST", base + "/approve"}, nil
	case MutationLineComment:
		line := mutation.Line
		if line.Path == "" || line.BaseSHA == "" || line.StartSHA == "" || line.HeadSHA == "" || (line.OldLine <= 0 && line.NewLine <= 0) {
			return nil, errors.New("GitLab line comment position is incomplete")
		}
		oldPath := line.PreviousPath
		if oldPath == "" {
			oldPath = line.Path
		}
		fields := [][2]string{
			{"body", mutation.Body}, {"position[position_type]", "text"},
			{"position[base_sha]", line.BaseSHA}, {"position[start_sha]", line.StartSHA}, {"position[head_sha]", line.HeadSHA},
			{"position[old_path]", oldPath}, {"position[new_path]", line.Path},
		}
		if line.OldLine > 0 {
			fields = append(fields, [2]string{"position[old_line]", strconv.Itoa(line.OldLine)})
		}
		if line.NewLine > 0 {
			fields = append(fields, [2]string{"position[new_line]", strconv.Itoa(line.NewLine)})
		}
		return writeAPIFields("POST", base+"/discussions", fields), nil
	default:
		return nil, fmt.Errorf("unsupported mutation %q", mutation.Kind)
	}
}

func writeAPIArgs(endpoint, field, value string) []string {
	return writeAPIArgsMethod("POST", endpoint, field, value)
}

func writeAPIArgsMethod(method, endpoint, field, value string) []string {
	return []string{"api", "--method", method, endpoint, "--raw-field", field + "=" + value}
}

func writeAPIFields(method, endpoint string, fields [][2]string) []string {
	args := []string{"api", "--method", method, endpoint}
	for _, field := range fields {
		args = append(args, "--raw-field", field[0]+"="+field[1])
	}
	return args
}

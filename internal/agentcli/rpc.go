package agentcli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"lazygitapp/internal/agentwork"
)

const (
	maxRPCRequestBytes  = 1 * 1024 * 1024
	maxRPCResponseBytes = 8 * 1024 * 1024
)

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Message string `json:"message"`
}

// ServeRPC serves one newline-delimited JSON response per non-empty request.
// Malformed requests are isolated to their response; clean EOF is not an error.
func (s *Service) ServeRPC(ctx context.Context, input io.Reader, output io.Writer) error {
	reader := bufio.NewReaderSize(input, 32*1024)
	for {
		line, oversized, err := readBoundedLine(reader, maxRPCRequestBytes)
		if errors.Is(err, io.EOF) && len(line) == 0 && !oversized {
			return nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read RPC request: %w", err)
		}
		if oversized {
			response := rpcResponse{ID: json.RawMessage("null"), Error: &rpcError{Message: fmt.Sprintf("RPC request exceeds %d bytes", maxRPCRequestBytes)}}
			if err := writeRPCResponse(output, response); err != nil {
				return err
			}
		} else if len(bytes.TrimSpace(line)) != 0 {
			response := s.handleRPC(ctx, line)
			if err := writeRPCResponse(output, response); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

// readBoundedLine discards the remainder of an oversized line so the stream
// can recover at its next newline without ever retaining an unbounded request.
func readBoundedLine(reader *bufio.Reader, limit int) ([]byte, bool, error) {
	line := make([]byte, 0, 4096)
	oversized := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if !oversized {
			if len(line)+len(fragment) > limit {
				oversized = true
				line = nil
			} else {
				line = append(line, fragment...)
			}
		}
		switch {
		case err == nil:
			return line, oversized, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return line, oversized, io.EOF
		default:
			return nil, false, err
		}
	}
}

func (s *Service) handleRPC(ctx context.Context, line []byte) rpcResponse {
	var request rpcRequest
	if err := json.Unmarshal(line, &request); err != nil {
		return rpcFailure(json.RawMessage("null"), fmt.Errorf("malformed RPC request: %w", err))
	}
	id := request.ID
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	result, err := s.callRPC(ctx, request.Method, request.Params)
	if err != nil {
		return rpcFailure(id, err)
	}
	return rpcResponse{ID: id, Result: result}
}

func rpcFailure(id json.RawMessage, err error) rpcResponse {
	return rpcResponse{ID: id, Error: &rpcError{Message: err.Error()}}
}

func writeRPCResponse(output io.Writer, response rpcResponse) error {
	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode RPC response: %w", err)
	}
	if len(data)+1 > maxRPCResponseBytes {
		oversized := rpcResponse{ID: response.ID, Error: &rpcError{Message: fmt.Sprintf("RPC response exceeds %d bytes", maxRPCResponseBytes)}}
		data, err = json.Marshal(oversized)
		if err != nil {
			return fmt.Errorf("encode RPC size error: %w", err)
		}
	}
	data = append(data, '\n')
	if _, err := output.Write(data); err != nil {
		return fmt.Errorf("write RPC response: %w", err)
	}
	return nil
}

func decodeRPCParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid RPC params: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid RPC params: trailing JSON value")
		}
		return fmt.Errorf("invalid RPC params: %w", err)
	}
	return nil
}

type rpcDataDir struct {
	DataDir string `json:"dataDir,omitempty"`
}

func (s *Service) callRPC(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "list":
		var params struct {
			rpcDataDir
			Repository string `json:"repository,omitempty"`
		}
		if err := decodeRPCParams(raw, &params); err != nil {
			return nil, err
		}
		service, err := s.forDataDir(params.DataDir)
		if err != nil {
			return nil, err
		}
		return service.List(ctx, params.Repository)

	case "show":
		var params struct {
			rpcDataDir
			TaskID string `json:"taskId"`
		}
		if err := decodeRPCParams(raw, &params); err != nil {
			return nil, err
		}
		service, err := s.forDataDir(params.DataDir)
		if err != nil {
			return nil, err
		}
		return service.Show(ctx, params.TaskID)

	case "diff":
		var params struct {
			rpcDataDir
			TaskID           string `json:"taskId"`
			File             string `json:"file,omitempty"`
			IgnoreWhitespace bool   `json:"ignoreWhitespace,omitempty"`
			ContextLines     int    `json:"contextLines,omitempty"`
		}
		if err := decodeRPCParams(raw, &params); err != nil {
			return nil, err
		}
		service, err := s.forDataDir(params.DataDir)
		if err != nil {
			return nil, err
		}
		return service.Diff(ctx, DiffOptions{
			TaskID: params.TaskID, File: params.File,
			IgnoreWhitespace: params.IgnoreWhitespace, ContextLines: params.ContextLines,
		})

	case "create":
		var params struct {
			rpcDataDir
			Repository string            `json:"repository"`
			Path       string            `json:"path"`
			Branch     string            `json:"branch"`
			Base       string            `json:"base"`
			Title      string            `json:"title"`
			Request    string            `json:"request"`
			Profile    agentwork.Profile `json:"profile,omitempty"`
			Yes        bool              `json:"yes"`
		}
		if err := decodeRPCParams(raw, &params); err != nil {
			return nil, err
		}
		service, err := s.forDataDir(params.DataDir)
		if err != nil {
			return nil, err
		}
		return service.Create(ctx, CreateParams{
			Repository: params.Repository, Path: params.Path, Branch: params.Branch,
			Base: params.Base, Title: params.Title, Request: params.Request,
			Profile: params.Profile, Yes: params.Yes,
		})

	case "report":
		var params struct {
			rpcDataDir
			TaskID            string           `json:"taskId"`
			ExpectedUpdatedAt string           `json:"expectedUpdatedAt"`
			SessionID         *string          `json:"sessionId,omitempty"`
			Summary           *string          `json:"summary,omitempty"`
			Status            agentwork.Status `json:"status,omitempty"`
		}
		if err := decodeRPCParams(raw, &params); err != nil {
			return nil, err
		}
		service, err := s.forDataDir(params.DataDir)
		if err != nil {
			return nil, err
		}
		return service.Report(ctx, ReportParams{
			TaskID: params.TaskID, ExpectedUpdatedAt: params.ExpectedUpdatedAt,
			SessionID: params.SessionID, Summary: params.Summary, Status: params.Status,
		})

	case "comment":
		var params struct {
			rpcDataDir
			TaskID            string `json:"taskId"`
			ExpectedUpdatedAt string `json:"expectedUpdatedAt"`
			Text              string `json:"text"`
		}
		if err := decodeRPCParams(raw, &params); err != nil {
			return nil, err
		}
		service, err := s.forDataDir(params.DataDir)
		if err != nil {
			return nil, err
		}
		return service.Comment(ctx, CommentParams{
			TaskID: params.TaskID, ExpectedUpdatedAt: params.ExpectedUpdatedAt, Text: params.Text,
		})

	case "test_result":
		var params struct {
			rpcDataDir
			TaskID            string               `json:"taskId"`
			ExpectedUpdatedAt string               `json:"expectedUpdatedAt"`
			Status            agentwork.TestStatus `json:"status"`
			Command           string               `json:"command"`
			ExitCode          *int                 `json:"exitCode"`
			StartedAt         time.Time            `json:"startedAt,omitempty"`
			FinishedAt        time.Time            `json:"finishedAt,omitempty"`
		}
		if err := decodeRPCParams(raw, &params); err != nil {
			return nil, err
		}
		if params.ExitCode == nil {
			return nil, errors.New("test_result exitCode is required and must be numeric")
		}
		service, err := s.forDataDir(params.DataDir)
		if err != nil {
			return nil, err
		}
		return service.RecordTestResult(ctx, TestResultParams{
			TaskID: params.TaskID, ExpectedUpdatedAt: params.ExpectedUpdatedAt,
			Status: params.Status, Command: params.Command, ExitCode: *params.ExitCode,
			StartedAt: params.StartedAt, FinishedAt: params.FinishedAt,
		})

	case "preview_patch", "apply_patch":
		var params struct {
			rpcDataDir
			Destination string `json:"destination"`
			Patch       string `json:"patch"`
			Yes         bool   `json:"yes,omitempty"`
		}
		if err := decodeRPCParams(raw, &params); err != nil {
			return nil, err
		}
		service, err := s.forDataDir(params.DataDir)
		if err != nil {
			return nil, err
		}
		patchParams := PatchParams{Destination: params.Destination, Patch: params.Patch, Yes: params.Yes}
		if method == "apply_patch" {
			return service.ApplyPatch(ctx, patchParams)
		}
		return service.PreviewPatch(ctx, patchParams)
	default:
		return nil, fmt.Errorf("unknown RPC method %q", method)
	}
}

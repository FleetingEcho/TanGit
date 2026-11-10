package agentcli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"lazygitapp/internal/agentwork"
)

func rpcLine(t *testing.T, id any, method string, params any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readRPCResponses(t *testing.T, output string) []struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
} {
	t.Helper()
	var responses []struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		var response struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatalf("invalid RPC output %q: %v", scanner.Text(), err)
		}
		responses = append(responses, response)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return responses
}

func TestRPCMalformedListAndReportFlow(t *testing.T) {
	root := newGitRepo(t)
	_, dataDir := newTestService(t)
	service, err := NewService(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	created := createStoredTask(t, service, root, nil)
	input := strings.Join([]string{
		`{"id":`,
		rpcLine(t, "list-1", "list", map[string]any{"dataDir": dataDir, "repository": root}),
		rpcLine(t, 2, "report", map[string]any{
			"dataDir": dataDir, "taskId": created.ID, "expectedUpdatedAt": revision(created),
			"summary": "RPC finished", "status": "ready_for_review",
		}),
		"",
	}, "\n")
	var output bytes.Buffer
	if err := service.ServeRPC(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := readRPCResponses(t, output.String())
	if len(responses) != 3 {
		t.Fatalf("responses = %d, output %q", len(responses), output.String())
	}
	if string(responses[0].ID) != "null" || responses[0].Error == nil || !strings.Contains(responses[0].Error.Message, "malformed RPC request") {
		t.Fatalf("malformed response = %#v", responses[0])
	}
	if responses[1].Error != nil || string(responses[1].ID) != `"list-1"` {
		t.Fatalf("list response = %#v", responses[1])
	}
	var tasks []agentwork.Task
	if err := json.Unmarshal(responses[1].Result, &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != created.ID {
		t.Fatalf("RPC list tasks = %#v", tasks)
	}
	if responses[2].Error != nil || string(responses[2].ID) != "2" {
		t.Fatalf("report response = %#v", responses[2])
	}
	var reported agentwork.Task
	if err := json.Unmarshal(responses[2].Result, &reported); err != nil {
		t.Fatal(err)
	}
	if reported.Summary != "RPC finished" || reported.Status != agentwork.StatusReadyForReview {
		t.Fatalf("RPC report task = %#v", reported)
	}
	stored, err := service.Show(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentwork.StatusReadyForReview || stored.Summary != "RPC finished" {
		t.Fatalf("RPC report was not persisted: %#v", stored)
	}
}

func TestRPCRejectsStaleRevisionAndUnknownMethodWithoutEndingStream(t *testing.T) {
	root := newGitRepo(t)
	service, dataDir := newTestService(t)
	created := createStoredTask(t, service, root, nil)
	input := strings.Join([]string{
		rpcLine(t, 1, "not_a_method", map[string]any{}),
		rpcLine(t, 2, "comment", map[string]any{
			"dataDir": dataDir, "taskId": created.ID,
			"expectedUpdatedAt": "2000-01-01T00:00:00Z", "text": "must not persist",
		}),
		rpcLine(t, 3, "show", map[string]any{"dataDir": dataDir, "taskId": created.ID}),
		"",
	}, "\n")
	var output bytes.Buffer
	if err := service.ServeRPC(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := readRPCResponses(t, output.String())
	if len(responses) != 3 || responses[0].Error == nil || responses[1].Error == nil || responses[2].Error != nil {
		t.Fatalf("RPC error continuation = %#v", responses)
	}
	if !strings.Contains(responses[1].Error.Message, "changed since") {
		t.Fatalf("stale RPC error = %q", responses[1].Error.Message)
	}
	var shown agentwork.Task
	if err := json.Unmarshal(responses[2].Result, &shown); err != nil {
		t.Fatal(err)
	}
	if shown.Request != created.Request {
		t.Fatalf("stale RPC mutation appended request: %q", shown.Request)
	}
}

func TestRPCOversizedRequestDoesNotTerminateStream(t *testing.T) {
	service, dataDir := newTestService(t)
	oversized := `{"id":1,"method":"list","params":{"dataDir":"` + dataDir + `","padding":"` + strings.Repeat("x", maxRPCRequestBytes) + `"}}`
	input := oversized + "\n" + rpcLine(t, 2, "list", map[string]any{"dataDir": dataDir}) + "\n"
	var output bytes.Buffer
	if err := service.ServeRPC(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := readRPCResponses(t, output.String())
	if len(responses) != 2 || responses[0].Error == nil || !strings.Contains(responses[0].Error.Message, "exceeds") || responses[1].Error != nil {
		t.Fatalf("oversized RPC recovery = %#v", responses)
	}
}

package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxLanguageServerMessage = 16 << 20

var errLanguageServerRequestTimedOut = errors.New("language server request timed out")

type languageServerConnection interface {
	DidOpen(uri, language, text string, version uint64)
	DidChange(uri, text string, version uint64)
	DidSave(uri string)
	DidClose(uri string)
	Request(method string, params any, result func(json.RawMessage, error))
	Done() <-chan error
	Close()
}

type languageServerLaunch struct {
	Program               string
	Arguments             []string
	Root                  string
	Env                   []string
	InitializationOptions map[string]any
	Notify                func(method string, params json.RawMessage)
}

var connectLanguageServer = connectStdioLanguageServer

// connectLanguageServerCandidates tries each configured executable until one
// completes initialization. A failed candidate never prevents a later local
// installation (for example one managed by Zed) from serving the workspace.
func connectLanguageServerCandidates(launch languageServerLaunch, programs []string) (languageServerConnection, error) {
	var failures []error
	for _, program := range programs {
		launch.Program = program
		connection, err := connectLanguageServer(launch)
		if err == nil {
			return connection, nil
		}
		if connection != nil {
			connection.Close()
		}
		failures = append(failures, fmt.Errorf("%s: %w", program, err))
	}
	if len(failures) == 0 {
		return nil, errors.New("no language server executable configured")
	}
	return nil, errors.Join(failures...)
}

type languageServerRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *languageServerRPCError) Error() string {
	return fmt.Sprintf("language server error %d: %s", e.Code, e.Message)
}

// ContentModified invalidates one request when the server's analysis changes.
// It does not indicate a broken server and should not be shown as a UI error.
func isLanguageServerContentModified(err error) bool {
	var rpcError *languageServerRPCError
	return errors.As(err, &rpcError) && rpcError.Code == -32801
}

// A feature request can time out while a server is still indexing a large
// workspace. The missing hover or completion is transient; the running server
// itself has not failed and must not be left in the error state.
func isLanguageServerRequestTimeout(err error) bool {
	return errors.Is(err, errLanguageServerRequestTimedOut)
}

type languageServerEnvelope struct {
	JSONRPC string                  `json:"jsonrpc"`
	ID      json.RawMessage         `json:"id,omitempty"`
	Method  string                  `json:"method,omitempty"`
	Params  json.RawMessage         `json:"params,omitempty"`
	Result  json.RawMessage         `json:"result,omitempty"`
	Error   *languageServerRPCError `json:"error,omitempty"`
}

type languageServerResponse struct {
	result json.RawMessage
	err    error
}

type languageServerWrite struct {
	data    []byte
	flushed chan error
}

type stdioLanguageServer struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	reader  *bufio.Reader

	writeMu  sync.Mutex
	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan languageServerResponse
	outgoing chan languageServerWrite
	done     chan error
	closed   chan struct{}
	once     sync.Once
	onNotify func(method string, params json.RawMessage)
}

func connectStdioLanguageServer(launch languageServerLaunch) (languageServerConnection, error) {
	command := exec.Command(launch.Program, launch.Arguments...)
	command.Dir = launch.Root
	if len(launch.Env) > 0 {
		command.Env = append(os.Environ(), launch.Env...)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	server := &stdioLanguageServer{
		command:  command,
		stdin:    stdin,
		reader:   bufio.NewReader(stdout),
		pending:  make(map[int64]chan languageServerResponse),
		outgoing: make(chan languageServerWrite, 256),
		done:     make(chan error, 1),
		closed:   make(chan struct{}),
		onNotify: launch.Notify,
	}
	go server.writeLoop()
	go server.readLoop()

	rootURI := languageServerFileURI(launch.Root)
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"workspace": map[string]any{"workspaceFolders": true},
			"window":    map[string]any{"workDoneProgress": true},
			"experimental": map[string]any{
				"serverStatusNotification": true,
			},
			"textDocument": map[string]any{
				"synchronization":    map[string]any{"didSave": true},
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition":         map[string]any{"linkSupport": true},
				"publishDiagnostics": map[string]any{"relatedInformation": true},
				"completion": map[string]any{"completionItem": map[string]any{
					"snippetSupport": true, "documentationFormat": []string{"markdown", "plaintext"},
				}},
			},
			"general": map[string]any{"positionEncodings": []string{"utf-16"}},
		},
		"workspaceFolders": []map[string]string{{"uri": rootURI, "name": filepathBase(launch.Root)}},
	}
	if launch.InitializationOptions != nil {
		params["initializationOptions"] = launch.InitializationOptions
	}
	if _, err := server.request("initialize", params, 8*time.Second); err != nil {
		server.Close()
		return nil, fmt.Errorf("initialize language server: %w", err)
	}
	server.notify("initialized", map[string]any{})
	return server, nil
}

func filepathBase(path string) string {
	path = strings.TrimRight(strings.ReplaceAll(path, "\\", "/"), "/")
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		return path[index+1:]
	}
	return path
}

func (s *stdioLanguageServer) Done() <-chan error { return s.done }

func (s *stdioLanguageServer) DidOpen(uri, language, text string, version uint64) {
	s.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": language, "version": version, "text": text,
	}})
}

func (s *stdioLanguageServer) DidChange(uri, text string, version uint64) {
	s.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": version},
		"contentChanges": []map[string]string{{"text": text}},
	})
}

func (s *stdioLanguageServer) DidSave(uri string) {
	s.notify("textDocument/didSave", map[string]any{"textDocument": map[string]string{"uri": uri}})
}

func (s *stdioLanguageServer) DidClose(uri string) {
	s.notify("textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": uri}})
}

func (s *stdioLanguageServer) Request(method string, params any, result func(json.RawMessage, error)) {
	go func() {
		response, err := s.request(method, params, 8*time.Second)
		if result != nil {
			result(response, err)
		}
	}()
}

func (s *stdioLanguageServer) request(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	answer := make(chan languageServerResponse, 1)
	s.pending[id] = answer
	s.mu.Unlock()
	if err := s.queue(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}, false); err != nil {
		s.removePending(id)
		return nil, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case response := <-answer:
		return response.result, response.err
	case <-timer.C:
		s.removePending(id)
		return nil, errLanguageServerRequestTimedOut
	case <-s.closed:
		return nil, errors.New("language server stopped")
	}
}

func (s *stdioLanguageServer) removePending(id int64) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func (s *stdioLanguageServer) notify(method string, params any) {
	_ = s.queue(map[string]any{"jsonrpc": "2.0", "method": method, "params": params}, false)
}

func (s *stdioLanguageServer) queue(message any, wait bool) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	select {
	case <-s.closed:
		return errors.New("language server stopped")
	default:
	}
	item := languageServerWrite{data: data}
	if wait {
		item.flushed = make(chan error, 1)
	}
	select {
	case s.outgoing <- item:
	case <-s.closed:
		return errors.New("language server stopped")
	}
	if item.flushed != nil {
		select {
		case err := <-item.flushed:
			return err
		case <-s.closed:
			return errors.New("language server stopped")
		}
	}
	return nil
}

func (s *stdioLanguageServer) writeLoop() {
	for {
		select {
		case item := <-s.outgoing:
			err := s.writeData(item.data)
			if item.flushed != nil {
				item.flushed <- err
			}
			if err != nil {
				_ = s.command.Process.Kill()
				return
			}
		case <-s.closed:
			return
		}
	}
}

func (s *stdioLanguageServer) writeData(data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := fmt.Fprintf(s.stdin, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err := s.stdin.Write(data)
	return err
}

func (s *stdioLanguageServer) readLoop() {
	for {
		message, err := readLanguageServerMessage(s.reader)
		if err != nil {
			s.finish(err)
			return
		}
		var envelope languageServerEnvelope
		if err := json.Unmarshal(message, &envelope); err != nil {
			continue
		}
		if len(envelope.ID) > 0 && envelope.Method == "" {
			var id int64
			if err := json.Unmarshal(envelope.ID, &id); err != nil {
				continue
			}
			s.mu.Lock()
			answer := s.pending[id]
			delete(s.pending, id)
			s.mu.Unlock()
			if answer != nil {
				response := languageServerResponse{result: envelope.Result}
				if envelope.Error != nil {
					response.err = envelope.Error
				}
				answer <- response
			}
			continue
		}
		if len(envelope.ID) > 0 && envelope.Method != "" {
			s.replyToServerRequest(envelope)
			continue
		}
		if envelope.Method != "" && s.onNotify != nil {
			s.onNotify(envelope.Method, envelope.Params)
		}
	}
}

func (s *stdioLanguageServer) replyToServerRequest(request languageServerEnvelope) {
	result := any(nil)
	if request.Method == "workspace/configuration" {
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(request.Params, &params)
		result = make([]any, len(params.Items))
	}
	var id any
	if err := json.Unmarshal(request.ID, &id); err != nil {
		return
	}
	_ = s.queue(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}, false)
}

func (s *stdioLanguageServer) finish(readErr error) {
	s.once.Do(func() {
		close(s.closed)
		s.mu.Lock()
		for id, answer := range s.pending {
			delete(s.pending, id)
			answer <- languageServerResponse{err: errors.New("language server stopped")}
		}
		s.mu.Unlock()
		waitErr := s.command.Wait()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			waitErr = readErr
		}
		s.done <- waitErr
		close(s.done)
	})
}

func (s *stdioLanguageServer) Close() {
	select {
	case <-s.closed:
		return
	default:
	}
	_, _ = s.request("shutdown", nil, time.Second)
	_ = s.queue(map[string]any{"jsonrpc": "2.0", "method": "exit"}, true)
	_ = s.stdin.Close()
	select {
	case <-s.done:
	case <-time.After(time.Second):
		_ = s.command.Process.Kill()
		select {
		case <-s.done:
		case <-time.After(time.Second):
		}
	}
}

func readLanguageServerMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if contentLength < 0 || contentLength > maxLanguageServerMessage {
		return nil, fmt.Errorf("invalid language server message length %d", contentLength)
	}
	message := make([]byte, contentLength)
	_, err := io.ReadFull(reader, message)
	return message, err
}

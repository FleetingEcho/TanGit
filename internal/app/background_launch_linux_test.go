//go:build linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

type detachedLaunchRecord struct {
	PID  int      `json:"pid"`
	Args []string `json:"args"`
}

func TestCommandLineLaunchReturnsWhileGUIStaysRunning(t *testing.T) {
	recordPath := filepath.Join(t.TempDir(), "child.json")
	command := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestDetachedLaunchHelper$", "--", "./")
	command.Env = append(os.Environ(),
		"MYGO_ENV=production",
		"TANGIT_DETACHED_LAUNCH_HELPER=1",
		"TANGIT_DETACHED_LAUNCH_RECORD="+recordPath,
	)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("launcher failed: %v\n%s", err, output.String())
		}
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		t.Fatalf("launcher remained attached to the GUI child\n%s", output.String())
	}

	var record detachedLaunchRecord
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(recordPath)
		if err == nil {
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			break
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("detached GUI child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-record.PID, syscall.SIGTERM)
	})
	if err := syscall.Kill(record.PID, 0); err != nil {
		t.Fatalf("GUI child exited with its launcher: %v", err)
	}
	if len(record.Args) < 2 || record.Args[len(record.Args)-2] != "--" || record.Args[len(record.Args)-1] != "./" {
		t.Fatalf("GUI child lost its command-line arguments: %v", record.Args)
	}
}

func TestDetachedLaunchHelper(t *testing.T) {
	if os.Getenv("TANGIT_DETACHED_LAUNCH_HELPER") != "1" {
		return
	}
	if os.Getenv(detachedLaunchEnvironment) == "1" {
		record, err := json.Marshal(detachedLaunchRecord{PID: os.Getpid(), Args: os.Args})
		if err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Getenv("TANGIT_DETACHED_LAUNCH_RECORD"), record, 0o600); err != nil {
			os.Exit(3)
		}
		// Stand in for the GUI event loop. The outer test terminates the new
		// session after confirming that the launcher returned and this is alive.
		select {}
	}
	launched, err := launchInBackground()
	if err != nil {
		t.Error(err)
	}
	if !launched {
		t.Error("production command-line launch was not detached")
	}
}

func TestDetachedChildDoesNotRelaunch(t *testing.T) {
	if os.Getenv("TANGIT_DETACHED_LAUNCH_HELPER") == "1" {
		t.Skip("helper process")
	}
	t.Setenv("MYGO_ENV", "production")
	t.Setenv(detachedLaunchEnvironment, "1")
	launched, err := launchInBackground()
	if err != nil || launched {
		t.Fatalf("detached child relaunched itself: launched=%v err=%v (pid %s)", launched, err, strconv.Itoa(os.Getpid()))
	}
}

package sshworkspace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestTransferCopyReportsCumulativeBytesAndItems(t *testing.T) {
	t.Parallel()
	var updates []TransferProgress
	state := transferState{ctx: context.Background(), progress: func(progress TransferProgress) {
		updates = append(updates, progress)
	}}
	var destination bytes.Buffer
	if err := state.copy(&destination, bytes.NewBufferString("streamed data"), "/remote/file"); err != nil {
		t.Fatal(err)
	}
	state.item("/remote/file")
	if destination.String() != "streamed data" {
		t.Fatalf("copied data = %q", destination.String())
	}
	last := updates[len(updates)-1]
	if last.Bytes != int64(len("streamed data")) || last.Items != 1 || last.Current != "/remote/file" {
		t.Fatalf("last progress = %+v", last)
	}
}

func TestTransferCopyHonorsCancellationAndShortWrites(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := transferState{ctx: ctx}
	if err := state.copy(io.Discard, bytes.NewBufferString("data"), "/remote/file"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled copy error = %v", err)
	}
	state.ctx = context.Background()
	if err := state.copy(shortWriter{}, bytes.NewBufferString("data"), "/remote/file"); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write error = %v", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

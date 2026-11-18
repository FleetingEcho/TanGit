package repository

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
)

const fileRecoveryEventAfterRecorded = "after-state-recorded"

// FileRecoveryWorkflowSession is the small application-facing extension used
// to discover durable records and to advance one record across several actions
// on the same conflict. The recovery algorithm itself remains behind
// FileRecoverySession.
type FileRecoveryWorkflowSession interface {
	FileRecoverySession
	ListFileRecovery(context.Context) ([]FileRecoveryRecord, error)
	CaptureFileRecoveryState(context.Context, string) (FileRecoveryState, error)
	RecordFileRecoveryAfter(context.Context, string, FileRecoveryState) (FileRecoveryRecord, error)
}

// CaptureFileRecoveryState returns exact bytes and Git stages for one path.
// Supplying this state as ArchiveFileRecovery.After is useful when a mutation
// is about to start: an unfinished action then has a conservative after-state.
func (s *gitSession) CaptureFileRecoveryState(ctx context.Context, relativePath string) (FileRecoveryState, error) {
	if err := ctx.Err(); err != nil {
		return FileRecoveryState{}, err
	}
	if err := validateFileRecoveryPath(relativePath); err != nil {
		return FileRecoveryState{}, err
	}
	state, err := s.capturePreparedFileRecoveryState(relativePath)
	if err != nil {
		return FileRecoveryState{}, fmt.Errorf("capture file recovery state for %s: %w", relativePath, err)
	}
	return state, nil
}

// ListFileRecovery discovers records after a repository is reopened. Manifest
// order is not used as recovery order: the most recent durable journal entry
// identifies the active record when several generations exist for one path.
func (s *gitSession) ListFileRecovery(ctx context.Context) ([]FileRecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := s.fileService().ReadDir(s.joinPath(s.fileRecoveryRoot(), "manifest"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list file recovery manifests: %w", err)
	}
	records := make([]FileRecoveryRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validOperationID(id) {
			continue
		}
		record, readErr := s.readFileRecoveryRecord(id, true)
		if os.IsNotExist(readErr) {
			// A concurrent directory cleanup may remove a record after ReadDir.
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("read file recovery %s: %w", id, readErr)
		}
		records = append(records, record)
	}
	sort.SliceStable(records, func(left, right int) bool {
		leftOrder, rightOrder := fileRecoveryListOrder(records[left]), fileRecoveryListOrder(records[right])
		if leftOrder != rightOrder {
			return leftOrder > rightOrder
		}
		return records[left].OperationID > records[right].OperationID
	})
	return records, nil
}

// RecordFileRecoveryAfter atomically advances only After while preserving the
// original unresolved Initial state and the restore journal. It is called after
// the app's mutation (including a failed mutation that left a partial state),
// so Restore's next CAS protects exactly what the user can now see.
func (s *gitSession) RecordFileRecoveryAfter(ctx context.Context, id string, after FileRecoveryState) (FileRecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return FileRecoveryRecord{}, err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()

	record, err := s.readFileRecoveryRecord(id, true)
	if err != nil {
		return FileRecoveryRecord{}, err
	}
	if record.Status != FileRecoveryReady || record.Progress != FileRecoveryPending {
		return FileRecoveryRecord{}, fmt.Errorf("file recovery %s is not ready for another resolution action", id)
	}
	after.Worktree, err = prepareFileRecoveryWorktree(after.Worktree)
	if err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("record after-state worktree: %w", err)
	}
	after.Index, err = s.prepareFileRecoveryIndex(after.Index)
	if err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("record after-state index: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return FileRecoveryRecord{}, err
	}
	if err := s.writeFileRecoveryStatePayloads(after); err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("write after-state file recovery payload: %w", err)
	}
	record.After = after
	appendFileRecoveryJournal(&record, fileRecoveryEventAfterRecorded, "")
	if err := s.writeFileRecoveryRecord(record); err != nil {
		return FileRecoveryRecord{}, err
	}
	stripFileRecoveryRecordContent(&record)
	return record, nil
}

func fileRecoveryListOrder(record FileRecoveryRecord) int64 {
	if len(record.Journal) > 0 {
		return record.Journal[len(record.Journal)-1].Time
	}
	return record.UpdatedAt
}

var _ FileRecoveryWorkflowSession = (*gitSession)(nil)

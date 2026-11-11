package repository

import "testing"

func TestParseRemoteStatusFiles(t *testing.T) {
	files, err := parseRemoteStatusFiles(" M tracked.txt\x00R  new name.txt\x00old name.txt\x00UU conflict.txt\x00?? untracked.bin\x00")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("files = %#v", files)
	}
	if files[0].Path != "tracked.txt" || !files[0].Tracked || !files[0].HasUnstagedChanges || files[0].HasStagedChanges {
		t.Errorf("modified file = %#v", files[0])
	}
	if files[1].Path != "new name.txt" || files[1].PreviousPath != "old name.txt" || !files[1].HasStagedChanges {
		t.Errorf("rename = %#v", files[1])
	}
	if !files[2].HasMergeConflicts || !files[2].HasInlineMergeConflicts {
		t.Errorf("conflict = %#v", files[2])
	}
	if files[3].Path != "untracked.bin" || files[3].Tracked || files[3].HasStagedChanges || !files[3].HasUnstagedChanges {
		t.Errorf("untracked = %#v", files[3])
	}
}

func TestParseRemoteStatusFilesRejectsTruncatedRename(t *testing.T) {
	if _, err := parseRemoteStatusFiles("R  new.txt\x00"); err == nil {
		t.Fatal("truncated rename was accepted")
	}
}

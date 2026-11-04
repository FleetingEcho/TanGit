package repository

import (
	"os"
	"testing"

	"github.com/jesseduffield/lazygit/pkg/app/daemon"
	"github.com/sirupsen/logrus"
)

// Interactive-rebase commands invoke the test executable as Git's editor too.
// Handle the same short-lived instructions as TanGit's main function before the
// generated test main starts running the suite.
func TestMain(m *testing.M) {
	if daemon.InDaemonMode() {
		daemon.Handle(logrus.New().WithField("component", "git-editor-test"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

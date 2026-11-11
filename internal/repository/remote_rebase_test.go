package repository

import (
	"testing"

	"github.com/stefanhaller/git-todo-parser/todo"
)

func TestRemoteRebaseTodoTransforms(t *testing.T) {
	todos := []todo.Todo{
		{Command: todo.Label, Label: "onto"},
		{Command: todo.Pick, Commit: "aaa111", Msg: "one"},
		{Command: todo.Pick, Commit: "bbb222", Msg: "two"},
		{Command: todo.Exec, ExecCommand: "make test"},
		{Command: todo.Pick, Commit: "ccc333", Msg: "three"},
	}
	changed, err := changeSessionTodoActions(append([]todo.Todo(nil), todos...), map[string]bool{"bbb222ffff": true}, todo.Fixup, "-C")
	if err != nil {
		t.Fatal(err)
	}
	if changed[2].Command != todo.Fixup || changed[2].Flag != "-C" || changed[1].Command != todo.Pick {
		t.Fatalf("changed todos = %#v", changed)
	}
	if _, err := changeSessionTodoActions(append([]todo.Todo(nil), todos...), map[string]bool{"fff000": true}, todo.Drop, ""); err == nil {
		t.Fatal("missing todo was accepted")
	}

	moved, err := moveSessionTodos(append([]todo.Todo(nil), todos...), []string{"aaa111", "bbb222"}, -1)
	if err != nil {
		t.Fatal(err)
	}
	var picks []string
	for _, item := range moved {
		if item.Command == todo.Pick {
			picks = append(picks, item.Commit)
		}
	}
	want := []string{"ccc333", "aaa111", "bbb222"}
	if len(picks) != len(want) {
		t.Fatalf("moved picks = %q", picks)
	}
	for i := range want {
		if picks[i] != want[i] {
			t.Fatalf("moved picks = %q, want %q", picks, want)
		}
	}
}

func TestRemoteRebaseMovesFixupAfterOriginal(t *testing.T) {
	todos := []todo.Todo{
		{Command: todo.Pick, Commit: "target", Msg: "target"},
		{Command: todo.Pick, Commit: "other", Msg: "other"},
		{Command: todo.Pick, Commit: "fixup", Msg: "fixup! target"},
	}
	moved, err := moveSessionFixup(todos, "target-long", "fixup-long", true)
	if err != nil {
		t.Fatal(err)
	}
	if moved[1].Commit != "fixup" || moved[1].Command != todo.Fixup || moved[2].Commit != "other" {
		t.Fatalf("fixup todos = %#v", moved)
	}
}

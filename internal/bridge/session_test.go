package bridge

import (
	"context"
	"os"
	"testing"
)

func TestResumeNativeIDRestoresDirectoryAcrossTopics(t *testing.T) {
	first, _, command := setupWorkspaceWorker(t)
	command("/new test")
	if first.client == nil || !validSessionID(first.sessionID) {
		t.Fatal("native session identity unavailable")
	}
	original, id := first.binding, first.sessionID
	ctx, cancel := context.WithCancel(first.ctx)
	second := &worker{b: first.b, key: target{chat: -10, thread: 22}, ctx: ctx, cancel: cancel, confirms: make(map[string]confirmation)}
	t.Cleanup(func() { second.shutdown(); cancel(); second.background.Wait(); second.drainMediaResults() })
	second.start(true, id, "")
	if second.client != nil {
		t.Fatal("same native session opened concurrently in another topic")
	}
	command("/close")
	second.start(true, id, "")
	if second.client == nil || second.binding.Session != original.Session || second.binding.Workspace != original.Workspace || second.sessionID != id {
		t.Fatal("ID resume did not restore original native session and cwd")
	}
	first.start(true, id, "")
	if first.client != nil {
		t.Fatal("native ID resume bypassed active session ownership")
	}
}

func TestInvalidResumeIDPreservesSavedSession(t *testing.T) {
	w, _, command := setupWorkspaceWorker(t)
	command("/new test")
	if w.client == nil {
		t.Fatal("session did not start")
	}
	command("/close")
	before := w.binding
	for _, id := range []string{"--print", "../../session.jsonl", "deadbeef-0000-4000-8000-000000000000"} {
		command("/resume " + id)
		if w.client != nil {
			t.Fatal("invalid ID started an instance")
		}
		saved, err := w.b.db.Binding(99, -10, 11)
		if err != nil || saved != before {
			t.Fatal("failed ID resume changed the saved binding")
		}
	}
}

func TestNativeIDResumeDoesNotRecreateMissingDirectory(t *testing.T) {
	w, _, command := setupWorkspaceWorker(t)
	command("/new test")
	if w.client == nil {
		t.Fatal("session did not start")
	}
	cwd, id := w.binding.Workspace, w.sessionID
	command("/close")
	if err := os.Remove(cwd); err != nil {
		t.Fatal(err)
	}
	command("/resume " + id)
	if w.client != nil {
		t.Fatal("native ID resumed without original directory")
	}
	if _, err := os.Stat(cwd); !os.IsNotExist(err) {
		t.Fatal("resume recreated a missing directory")
	}
}

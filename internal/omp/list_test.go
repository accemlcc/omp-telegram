package omp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The child reads only explicit fixture metadata from its controlled temp cwd.
func fixtureACP() {
	if len(os.Args) == 2 && os.Getenv("OMP_LIST_DESCENDANT") == "1" {
		signal.Ignore(syscall.SIGTERM)
		for {
			time.Sleep(time.Hour)
		}
	}
	modeBytes, _ := os.ReadFile("fixture-mode")
	mode := string(modeBytes)
	_ = os.WriteFile("fixture-pid", []byte(strconv.Itoa(os.Getpid())), 0600)
	args, _ := json.Marshal(os.Args[1:])
	_ = os.WriteFile("fixture-args", args, 0600)
	input := bufio.NewReader(os.Stdin)
	encode := json.NewEncoder(os.Stdout)
	page := 0
	for {
		line, err := input.ReadBytes('\n')
		if err != nil {
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(line, &request) != nil {
			os.Exit(11)
		}
		if request.Method == "" {
			if string(request.ID) != `"server-request"` || len(request.Error) == 0 {
				os.Exit(12)
			}
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			if request.Params["protocolVersion"] != float64(1) {
				os.Exit(13)
			}
			caps := map[string]any{"list": map[string]any{}}
			if mode == "unsupported" {
				delete(caps, "list")
			}
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"sessionCapabilities": caps}}
		case "session/list":
			cwd, _ := os.Getwd()
			if request.Params["cwd"] != cwd {
				os.Exit(14)
			}
			if mode == "pages" {
				if page == 0 && request.Params["cursor"] != nil {
					os.Exit(18)
				}
				if page == 1 && request.Params["cursor"] != "page-two" {
					os.Exit(19)
				}
			}
			if mode == "cancel" {
				signal.Ignore(syscall.SIGTERM)
				child := exec.Command(os.Args[0], "acp")
				child.Env = append(os.Environ(), "OMP_LIST_DESCENDANT=1")
				child.Stdout, child.Stderr = os.Stdout, os.Stderr
				if child.Start() != nil {
					os.Exit(15)
				}
				_ = os.WriteFile("fixture-child-pid", []byte(strconv.Itoa(child.Process.Pid)), 0600)
				for {
					time.Sleep(time.Hour)
				}
			}
			switch mode {
			case "malformed":
				fmt.Println("sensitive malformed diagnostics")
				return
			case "oversized":
				fmt.Println(strings.Repeat("sensitive", maxListFrame))
				return
			case "error":
				_ = encode.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32603, "message": "sensitive native details"}})
				continue
			case "wrong-id":
				request.ID = json.RawMessage(`"unrelated"`)
			}
			metadata, err := os.ReadFile(fmt.Sprintf("fixture-page-%d", page))
			if err != nil {
				os.Exit(16)
			}
			result = json.RawMessage(metadata)
			if mode != "repeat" {
				page++
			}
		default:
			os.Exit(17) // In particular, never load/create/prompt.
		}
		_ = encode.Encode(map[string]any{"jsonrpc": "2.0", "method": "notification", "params": map[string]any{}})
		_ = encode.Encode(map[string]any{"jsonrpc": "2.0", "method": "unsupported", "id": "server-request"})
		_ = encode.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
}

func listFixture(t *testing.T, mode string, pages ...any) Config {
	t.Helper()
	cwd := t.TempDir()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "fixture-mode"), []byte(mode), 0600); err != nil {
		t.Fatal(err)
	}
	for i, page := range pages {
		data, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cwd, fmt.Sprintf("fixture-page-%d", i)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return Config{Binary: binary, CWD: cwd}
}

func writeListPage(t *testing.T, cfg Config, index int, sessions []map[string]any, cursor string) {
	t.Helper()
	page := map[string]any{"sessions": sessions}
	if cursor != "" {
		page["nextCursor"] = cursor
	}
	data, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.CWD, fmt.Sprintf("fixture-page-%d", index)), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestListSessionsNativePagination(t *testing.T) {
	cfg := listFixture(t, "pages")
	alias := filepath.Join(t.TempDir(), "workspace-alias")
	if err := os.Symlink(cfg.CWD, alias); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	writeListPage(t, cfg, 0, []map[string]any{
		{"sessionId": "newest opaque ID", "cwd": alias, "title": "Named", "updatedAt": "2026-09-16T12:00:00Z", "transcript": "must not escape"},
		{"sessionId": "other", "cwd": other},
	}, "page-two")
	writeListPage(t, cfg, 1, []map[string]any{
		{"sessionId": "newest opaque ID", "cwd": cfg.CWD, "title": "duplicate"},
		{"sessionId": "older", "cwd": cfg.CWD},
	}, "")
	cfg.Args = []string{"--session-dir", filepath.Join(cfg.CWD, "native-store"), "--settings", "explicit-settings.json"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := ListSessions(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []SessionSummary{{"newest opaque ID", alias, "Named", "2026-09-16T12:00:00Z"}, {"older", cfg.CWD, "", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sessions = %#v, want %#v", got, want)
	}
	data, err := os.ReadFile(filepath.Join(cfg.CWD, "fixture-args"))
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatal(err)
	}
	if want := append([]string{"acp", "--cwd", cfg.CWD}, cfg.Args...); !reflect.DeepEqual(args, want) {
		t.Fatalf("native CLI args changed: %v", args)
	}
	assertListReaped(t, cfg)
}

func assertListReaped(t *testing.T, cfg Config) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cfg.CWD, "fixture-pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("listing process not reaped: %v", err)
	}
}

func TestListSessionsRejectsUnsafeResponses(t *testing.T) {
	for _, mode := range []string{"unsupported", "repeat", "oversized", "malformed", "error", "wrong-id", "missing-sessions", "too-many", "empty-id"} {
		t.Run(mode, func(t *testing.T) {
			cfg := listFixture(t, mode)
			sessions := []map[string]any{}
			cursor := ""
			if mode == "repeat" {
				cursor = "again"
			}
			if mode == "too-many" {
				for i := 0; i <= maxListEntries; i++ {
					sessions = append(sessions, map[string]any{"sessionId": "same", "cwd": cfg.CWD})
				}
			}
			if mode == "empty-id" {
				sessions = append(sessions, map[string]any{"sessionId": "", "cwd": cfg.CWD})
			}
			writeListPage(t, cfg, 0, sessions, cursor)
			if mode == "missing-sessions" {
				if err := os.WriteFile(filepath.Join(cfg.CWD, "fixture-page-0"), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			got, err := ListSessions(ctx, cfg)
			if err == nil || got != nil {
				t.Fatalf("unsafe response accepted: %#v %v", got, err)
			}
			if strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("raw diagnostic leaked: %v", err)
			}
			assertListReaped(t, cfg)
		})
	}
}

func TestListSessionsCancellationReapsProcessGroup(t *testing.T) {
	cfg := listFixture(t, "cancel")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := ListSessions(ctx, cfg); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	var childPID int
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(cfg.CWD, "fixture-child-pid"))
		if err == nil {
			childPID, _ = strconv.Atoi(string(data))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("cancel error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("listing cancellation did not finish")
	}
	if childPID == 0 {
		t.Fatal("fixture did not start descendant")
	}
	assertListReaped(t, cfg)
	// Orphaned descendants can briefly remain zombies until the host init reaps them.
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", childPID))
		if os.IsNotExist(err) || (err == nil && strings.Contains(string(stat), ") Z ")) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("listing descendant is still running")
}

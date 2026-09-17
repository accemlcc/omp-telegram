package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schemaVersion = 1

type Store struct{ DB *sql.DB }
type Binding struct {
	Bot, Chat, Thread  int64
	Workspace, Session string
	Generation         int64
	Running            bool
}
type Input struct {
	ID  int64
	Raw json.RawMessage
}
type Output struct {
	ID, Chat, Thread int64
	Text             string
	Kind, Path, Name string
}

func Open(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	// Existing directories may be the user's project root; preserve their permissions.
	path := filepath.Join(dir, "omp-telegram.db")
	if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database must be a regular file")
	} else if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return nil, e
	}
	if e = f.Close(); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA busy_timeout=5000;`)
	if e == nil {
		e = initialize(db)
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db}, nil
}

func initialize(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 0 && version != schemaVersion {
		return fmt.Errorf("unsupported database schema version %d; this binary supports version %d", version, schemaVersion)
	}
	if version == 0 {
		var populated bool
		if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%')").Scan(&populated); err != nil {
			return err
		}
		if populated {
			return fmt.Errorf("unversioned database is unsupported; back it up and use a fresh data directory")
		}
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return err
	}
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if version == 0 {
		_, e = tx.Exec(`
 CREATE TABLE meta (key TEXT PRIMARY KEY,value INTEGER NOT NULL);
 CREATE TABLE bindings(bot INTEGER,chat INTEGER,thread INTEGER,workspace TEXT NOT NULL,session TEXT NOT NULL,generation INTEGER NOT NULL,running INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(bot,chat,thread));
 CREATE TABLE history(bot INTEGER,chat INTEGER,thread INTEGER,workspace TEXT,session TEXT,generation INTEGER);
 CREATE TABLE inbox(id INTEGER PRIMARY KEY,raw BLOB NOT NULL,state TEXT NOT NULL);
 CREATE TABLE outbox(id INTEGER PRIMARY KEY AUTOINCREMENT,chat INTEGER,thread INTEGER,text TEXT NOT NULL,state TEXT NOT NULL,kind TEXT NOT NULL DEFAULT 'text',path TEXT NOT NULL DEFAULT '',name TEXT NOT NULL DEFAULT '');
 CREATE INDEX idx_inbox_state ON inbox(state,id);
 CREATE INDEX idx_outbox_state ON outbox(state,id);`)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion)); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(`UPDATE inbox SET state='uncertain' WHERE state='submitted';
 UPDATE outbox SET state='uncertain' WHERE state='sending';`); e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Offset() (int64, error) {
	var n int64
	e := s.DB.QueryRow("SELECT value FROM meta WHERE key='offset'").Scan(&n)
	if e == sql.ErrNoRows {
		return 0, nil
	}
	return n, e
}
func (s *Store) CheckBot(id int64) error {
	if _, e := s.DB.Exec("INSERT INTO meta(key,value) VALUES('bot',?) ON CONFLICT(key) DO NOTHING", id); e != nil {
		return e
	}
	var old int64
	e := s.DB.QueryRow("SELECT value FROM meta WHERE key='bot'").Scan(&old)
	if e == nil && old != id {
		return fmt.Errorf("data directory belongs to another bot")
	}
	return e
}
func (s *Store) Accept(id int64, raw []byte) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO inbox VALUES(?,?,'pending') ON CONFLICT(id) DO NOTHING", id, raw); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO meta VALUES('offset',?) ON CONFLICT(key) DO UPDATE SET value=MAX(value,excluded.value)", id+1); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Pending() ([]Input, error) {
	rows, e := s.DB.Query("SELECT id,raw FROM inbox WHERE state='pending' ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Input
	for rows.Next() {
		var v Input
		if e = rows.Scan(&v.ID, &v.Raw); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Mark(id int64, state string) error {
	_, e := s.DB.Exec("UPDATE inbox SET state=? WHERE id=?", state, id)
	return e
}
func (s *Store) Binding(bot, chat, thread int64) (Binding, error) {
	b := Binding{Bot: bot, Chat: chat, Thread: thread}
	e := s.DB.QueryRow("SELECT workspace,session,generation,running FROM bindings WHERE bot=? AND chat=? AND thread=?", bot, chat, thread).Scan(&b.Workspace, &b.Session, &b.Generation, &b.Running)
	return b, e
}
func (s *Store) Save(b Binding) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec("INSERT INTO history(bot,chat,thread,workspace,session,generation) SELECT bot,chat,thread,workspace,session,generation FROM bindings WHERE bot=? AND chat=? AND thread=?", b.Bot, b.Chat, b.Thread)
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO bindings(bot,chat,thread,workspace,session,generation,running) VALUES(?,?,?,?,?,?,?) ON CONFLICT(bot,chat,thread) DO UPDATE SET workspace=excluded.workspace,session=excluded.session,generation=excluded.generation,running=excluded.running", b.Bot, b.Chat, b.Thread, b.Workspace, b.Session, b.Generation, b.Running)
	if e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) RunningBindings(bot int64) ([]Binding, error) {
	rows, e := s.DB.Query("SELECT bot,chat,thread,workspace,session,generation,running FROM bindings WHERE bot=? AND running=1 ORDER BY chat,thread", bot)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var bindings []Binding
	for rows.Next() {
		var b Binding
		if e = rows.Scan(&b.Bot, &b.Chat, &b.Thread, &b.Workspace, &b.Session, &b.Generation, &b.Running); e != nil {
			return nil, e
		}
		bindings = append(bindings, b)
	}
	return bindings, rows.Err()
}

func (s *Store) SetRunning(b Binding, running bool) error {
	_, e := s.DB.Exec("UPDATE bindings SET running=? WHERE bot=? AND chat=? AND thread=? AND generation=?", running, b.Bot, b.Chat, b.Thread, b.Generation)
	return e
}

func (s *Store) Enqueue(chat, thread int64, text string) error {
	_, e := s.DB.Exec("INSERT INTO outbox(chat,thread,text,state) VALUES(?,?,?,'pending')", chat, thread, text)
	return e
}

// CompleteInboxWithReplies commits the complete final result and input completion together.
// A failed transaction leaves the submitted input and outbox unchanged.
func (s *Store) CompleteInboxWithReplies(ctx context.Context, id, chat, thread int64, replies []string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE inbox SET state='done' WHERE id=? AND state='submitted'", id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("input is not submitted")
	}
	for _, reply := range replies {
		if _, err := tx.ExecContext(ctx, "INSERT INTO outbox(chat,thread,text,state) VALUES(?,?,?,'pending')", chat, thread, reply); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) EnqueueAttachment(chat, thread int64, kind, path, name, caption string) error {
	if kind != "photo" && kind != "document" {
		return fmt.Errorf("unsupported attachment kind %q", kind)
	}
	_, e := s.DB.Exec("INSERT INTO outbox(chat,thread,text,state,kind,path,name) VALUES(?,?,?,'pending',?,?,?)", chat, thread, caption, kind, path, name)
	return e
}

func (s *Store) NextOutput() (Output, error) {
	var o Output
	e := s.DB.QueryRow("SELECT id,chat,thread,text,kind,path,name FROM outbox WHERE state='pending' ORDER BY id LIMIT 1").Scan(&o.ID, &o.Chat, &o.Thread, &o.Text, &o.Kind, &o.Path, &o.Name)
	return o, e
}
func (s *Store) MarkOutput(id int64, state string) error {
	_, e := s.DB.Exec("UPDATE outbox SET state=? WHERE id=?", state, id)
	return e
}
func (s *Store) Uncertain() (int, error) {
	var n int
	e := s.DB.QueryRow("SELECT (SELECT COUNT(*) FROM inbox WHERE state='uncertain')+(SELECT COUNT(*) FROM outbox WHERE state='uncertain')").Scan(&n)
	return n, e
}

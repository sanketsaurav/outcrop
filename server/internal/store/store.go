// Package store persists notes, assets, and theme in SQLite, and blobs on disk.
package store

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrSlugTaken    = errors.New("slug already taken")
	ErrUnknownAsset = errors.New("note references an asset hash that was never uploaded")
)

const pbkdf2Iters = 210_000

type Store struct {
	db *sql.DB
}

const schemaV1 = `
CREATE TABLE notes (
  id            TEXT PRIMARY KEY,
  slug          TEXT NOT NULL UNIQUE,
  title         TEXT NOT NULL,
  description   TEXT NOT NULL DEFAULT '',
  html          TEXT NOT NULL,
  noindex       INTEGER NOT NULL DEFAULT 0,
  passcode_hash BLOB,
  passcode_salt BLOB,
  passcode_gen  INTEGER NOT NULL DEFAULT 0,
  size_bytes    INTEGER NOT NULL,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

CREATE TABLE assets (
  hash        TEXT PRIMARY KEY,
  ext         TEXT NOT NULL,
  mime        TEXT NOT NULL,
  size_bytes  INTEGER NOT NULL,
  created_at  INTEGER NOT NULL
);

CREATE TABLE note_assets (
  note_id TEXT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  hash    TEXT NOT NULL REFERENCES assets(hash),
  PRIMARY KEY (note_id, hash)
);

CREATE TABLE theme (
  key        TEXT PRIMARY KEY,
  content    TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

func Open(path string) (*Store, error) {
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	migrations := []string{schemaV1} // index i migrates user_version i → i+1
	for ; v < len(migrations); v++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[v]); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---- Notes ----

type Note struct {
	ID           string
	Slug         string
	Title        string
	Description  string
	HTML         string
	Noindex      bool
	PasscodeHash []byte
	PasscodeSalt []byte
	PasscodeGen  int64
	SizeBytes    int64
	CreatedAt    int64
	UpdatedAt    int64
}

func (n *Note) Protected() bool { return len(n.PasscodeHash) > 0 }

type NoteInput struct {
	Title       string
	Description string
	HTML        string
	Noindex     bool
	Assets      []string
}

const noteCols = "id, slug, title, description, html, noindex, passcode_hash, passcode_salt, passcode_gen, size_bytes, created_at, updated_at"

func scanNote(row interface{ Scan(...any) error }) (*Note, error) {
	var n Note
	var noindex int
	err := row.Scan(&n.ID, &n.Slug, &n.Title, &n.Description, &n.HTML, &noindex,
		&n.PasscodeHash, &n.PasscodeSalt, &n.PasscodeGen, &n.SizeBytes, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	n.Noindex = noindex != 0
	return &n, nil
}

func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func isFKErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

func (s *Store) CreateNote(in NoteInput, customSlug string) (*Note, error) {
	id := NewID()
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for attempt := 0; ; attempt++ {
		slug := customSlug
		if slug == "" {
			slug = NewSlug()
		}
		_, err = tx.Exec(`INSERT INTO notes (id, slug, title, description, html, noindex, size_bytes, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, slug, in.Title, in.Description, in.HTML, boolInt(in.Noindex), int64(len(in.HTML)), now, now)
		if err == nil {
			break
		}
		if isUniqueErr(err) {
			if customSlug != "" {
				return nil, ErrSlugTaken
			}
			if attempt < 3 {
				continue // astronomically unlikely random collision
			}
		}
		return nil, err
	}
	if err := replaceNoteAssets(tx, id, in.Assets); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetNote(id)
}

// UpdateNote replaces a note's content and asset references. It returns the
// updated note plus the hashes of assets that became unreferenced (for blob GC).
func (s *Store) UpdateNote(id string, in NoteInput) (*Note, []string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE notes SET title=?, description=?, html=?, noindex=?, size_bytes=?, updated_at=? WHERE id=?`,
		in.Title, in.Description, in.HTML, boolInt(in.Noindex), int64(len(in.HTML)), time.Now().Unix(), id)
	if err != nil {
		return nil, nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil, ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM note_assets WHERE note_id=?`, id); err != nil {
		return nil, nil, err
	}
	if err := replaceNoteAssets(tx, id, in.Assets); err != nil {
		return nil, nil, err
	}
	gc, err := gcAssets(tx)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	note, err := s.GetNote(id)
	return note, gc, err
}

func (s *Store) DeleteNote(id string) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`DELETE FROM notes WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	gc, err := gcAssets(tx)
	if err != nil {
		return nil, err
	}
	return gc, tx.Commit()
}

func (s *Store) RotateSlug(id, customSlug string) (*Note, error) {
	for attempt := 0; ; attempt++ {
		slug := customSlug
		if slug == "" {
			slug = NewSlug()
		}
		res, err := s.db.Exec(`UPDATE notes SET slug=?, updated_at=? WHERE id=?`, slug, time.Now().Unix(), id)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, ErrNotFound
			}
			return s.GetNote(id)
		}
		if isUniqueErr(err) {
			if customSlug != "" {
				// Rotating to the slug the note already has is a no-op, not a conflict.
				if note, gerr := s.GetNote(id); gerr == nil && note.Slug == customSlug {
					return note, nil
				}
				return nil, ErrSlugTaken
			}
			if attempt < 3 {
				continue
			}
		}
		return nil, err
	}
}

func (s *Store) GetNote(id string) (*Note, error) {
	return scanNote(s.db.QueryRow(`SELECT `+noteCols+` FROM notes WHERE id=?`, id))
}

func (s *Store) GetNoteBySlug(slug string) (*Note, error) {
	return scanNote(s.db.QueryRow(`SELECT `+noteCols+` FROM notes WHERE slug=?`, slug))
}

// ListNotes returns all notes newest-updated first, without their HTML bodies.
func (s *Store) ListNotes() ([]*Note, error) {
	rows, err := s.db.Query(`SELECT ` + noteCols + ` FROM notes ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		n.HTML = ""
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListSitemapNotes returns slug+updated_at for notes that are public and indexable.
func (s *Store) ListSitemapNotes() ([]*Note, error) {
	rows, err := s.db.Query(`SELECT slug, updated_at FROM notes
		WHERE noindex=0 AND passcode_hash IS NULL ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.Slug, &n.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	return out, rows.Err()
}

func (s *Store) CountNotes() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&n)
	return n, err
}

func replaceNoteAssets(tx *sql.Tx, noteID string, hashes []string) error {
	seen := map[string]bool{}
	for _, h := range hashes {
		if seen[h] {
			continue
		}
		seen[h] = true
		if _, err := tx.Exec(`INSERT INTO note_assets (note_id, hash) VALUES (?, ?)`, noteID, h); err != nil {
			if isFKErr(err) {
				return ErrUnknownAsset
			}
			return err
		}
	}
	return nil
}

// gcAssets deletes asset rows no note references anymore and returns their hashes.
func gcAssets(tx *sql.Tx) ([]string, error) {
	rows, err := tx.Query(`SELECT hash FROM assets WHERE hash NOT IN (SELECT hash FROM note_assets)`)
	if err != nil {
		return nil, err
	}
	var gone []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return nil, err
		}
		gone = append(gone, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(gone) > 0 {
		if _, err := tx.Exec(`DELETE FROM assets WHERE hash NOT IN (SELECT hash FROM note_assets)`); err != nil {
			return nil, err
		}
	}
	return gone, nil
}

// ---- Passcodes ----

func NormalizePasscode(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func hashPasscode(passcode string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passcode, salt, pbkdf2Iters, 32)
}

func (s *Store) SetPasscode(id, passcode string) error {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	hash, err := hashPasscode(NormalizePasscode(passcode), salt)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE notes SET passcode_hash=?, passcode_salt=?, passcode_gen=passcode_gen+1, updated_at=? WHERE id=?`,
		hash, salt, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ClearPasscode(id string) error {
	res, err := s.db.Exec(`UPDATE notes SET passcode_hash=NULL, passcode_salt=NULL, passcode_gen=passcode_gen+1, updated_at=? WHERE id=? AND passcode_hash IS NOT NULL`,
		time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Distinguish "no such note" from the idempotent "already unprotected".
		if _, err := s.GetNote(id); err != nil {
			return err
		}
	}
	return nil
}

// VerifyPasscode checks a candidate passcode against a note in constant time.
func VerifyPasscode(n *Note, passcode string) bool {
	if !n.Protected() {
		return false
	}
	h, err := hashPasscode(NormalizePasscode(passcode), n.PasscodeSalt)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(h, n.PasscodeHash) == 1
}

// UnlockToken derives the cookie value proving a successful unlock. It binds
// the note identity and the passcode generation, so changing the passcode
// invalidates every outstanding cookie.
func UnlockToken(secret []byte, n *Note) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%s:%d", n.ID, n.PasscodeGen)
	return hex.EncodeToString(mac.Sum(nil))
}

// ---- Assets ----

type Asset struct {
	Hash      string
	Ext       string
	MIME      string
	SizeBytes int64
	CreatedAt int64
}

func (s *Store) InsertAsset(hash, ext, mime string, size int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO assets (hash, ext, mime, size_bytes, created_at) VALUES (?, ?, ?, ?, ?)`,
		hash, ext, mime, size, time.Now().Unix())
	return err
}

func (s *Store) GetAsset(hash string) (*Asset, error) {
	var a Asset
	err := s.db.QueryRow(`SELECT hash, ext, mime, size_bytes, created_at FROM assets WHERE hash=?`, hash).
		Scan(&a.Hash, &a.Ext, &a.MIME, &a.SizeBytes, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) AssetExists(hash string) (bool, error) {
	_, err := s.GetAsset(hash)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// ---- Theme ----

type Theme struct {
	CSS       string
	JS        string
	Head      string
	UpdatedAt int64
}

func (s *Store) SetTheme(t Theme) error {
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, content := range map[string]string{"css": t.CSS, "js": t.JS, "head": t.Head} {
		if _, err := tx.Exec(`INSERT INTO theme (key, content, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
			key, content, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetTheme() (Theme, error) {
	rows, err := s.db.Query(`SELECT key, content, updated_at FROM theme`)
	if err != nil {
		return Theme{}, err
	}
	defer rows.Close()
	var t Theme
	for rows.Next() {
		var key, content string
		var at int64
		if err := rows.Scan(&key, &content, &at); err != nil {
			return Theme{}, err
		}
		switch key {
		case "css":
			t.CSS = content
		case "js":
			t.JS = content
		case "head":
			t.Head = content
		}
		if at > t.UpdatedAt {
			t.UpdatedAt = at
		}
	}
	return t, rows.Err()
}

// ---- Meta ----

// CookieSecret returns the persistent secret used to sign unlock cookies,
// generating it on first use.
func (s *Store) CookieSecret() ([]byte, error) {
	for {
		var v string
		err := s.db.QueryRow(`SELECT value FROM meta WHERE key='cookie_secret'`).Scan(&v)
		if err == nil {
			return hex.DecodeString(v)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO meta (key, value) VALUES ('cookie_secret', ?)`,
			hex.EncodeToString(b)); err != nil {
			return nil, err
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

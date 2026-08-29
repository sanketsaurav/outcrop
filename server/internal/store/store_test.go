package store

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustCreate(t *testing.T, s *Store, in NoteInput, slug string) *Note {
	t.Helper()
	n, err := s.CreateNote(in, slug)
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	return n
}

func TestNoteLifecycleAndAssetGC(t *testing.T) {
	s := newTestStore(t)
	h1 := strings.Repeat("a", 64)
	h2 := strings.Repeat("b", 64)
	for _, h := range []string{h1, h2} {
		if err := s.InsertAsset(h, "png", "image/png", 3); err != nil {
			t.Fatalf("insert asset: %v", err)
		}
	}

	n := mustCreate(t, s, NoteInput{Title: "Hello", HTML: "<p>hi</p>", Assets: []string{h1, h2}}, "")
	if len(n.Slug) != 10 {
		t.Fatalf("expected 10-char random slug, got %q", n.Slug)
	}
	if got, err := s.GetNoteBySlug(n.Slug); err != nil || got.ID != n.ID {
		t.Fatalf("get by slug: %v", err)
	}

	// Dropping h2 from the note must garbage-collect it.
	_, gc, err := s.UpdateNote(n.ID, NoteInput{Title: "Hello2", HTML: "<p>hi2</p>", Assets: []string{h1}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !slices.Contains(gc, h2) || slices.Contains(gc, h1) {
		t.Fatalf("expected gc of %s only, got %v", h2, gc)
	}
	if exists, _ := s.AssetExists(h2); exists {
		t.Fatal("h2 should be gone")
	}

	// Unknown asset reference is rejected.
	if _, _, err := s.UpdateNote(n.ID, NoteInput{Title: "x", HTML: "y", Assets: []string{strings.Repeat("c", 64)}}); err != ErrUnknownAsset {
		t.Fatalf("expected ErrUnknownAsset, got %v", err)
	}

	gc, err = s.DeleteNote(n.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !slices.Contains(gc, h1) {
		t.Fatalf("expected h1 gc'd on delete, got %v", gc)
	}
	if _, err := s.GetNote(n.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestRotateSlug(t *testing.T) {
	s := newTestStore(t)
	a := mustCreate(t, s, NoteInput{Title: "A", HTML: "a"}, "")
	b := mustCreate(t, s, NoteInput{Title: "B", HTML: "b"}, "taken-slug")

	old := a.Slug
	rotated, err := s.RotateSlug(a.ID, "")
	if err != nil || rotated.Slug == old {
		t.Fatalf("rotate should change slug: %v (slug %q)", err, rotated.Slug)
	}
	if _, err := s.GetNoteBySlug(old); err != ErrNotFound {
		t.Fatal("old slug should be dead")
	}

	if _, err := s.RotateSlug(a.ID, "taken-slug"); err != ErrSlugTaken {
		t.Fatalf("expected ErrSlugTaken, got %v", err)
	}
	// Rotating to your own current slug is a no-op, not a conflict.
	if same, err := s.RotateSlug(b.ID, "taken-slug"); err != nil || same.Slug != "taken-slug" {
		t.Fatalf("self-rotate: %v", err)
	}
	if _, err := s.RotateSlug("nope", ""); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPasscode(t *testing.T) {
	s := newTestStore(t)
	n := mustCreate(t, s, NoteInput{Title: "Secret", HTML: "s"}, "")
	if n.Protected() {
		t.Fatal("new note should be unprotected")
	}

	if err := s.SetPasscode(n.ID, "Amber-Falcon-42 "); err != nil {
		t.Fatalf("set passcode: %v", err)
	}
	n, _ = s.GetNote(n.ID)
	if !n.Protected() || n.PasscodeGen != 1 {
		t.Fatalf("expected protected gen=1, got gen=%d", n.PasscodeGen)
	}
	// Verification is normalization-insensitive.
	if !VerifyPasscode(n, "amber-falcon-42") || !VerifyPasscode(n, "  AMBER-FALCON-42") {
		t.Fatal("correct passcode rejected")
	}
	if VerifyPasscode(n, "wrong-pass-11") {
		t.Fatal("wrong passcode accepted")
	}

	tok1 := UnlockToken([]byte("secret"), n)
	if err := s.SetPasscode(n.ID, "new-pass-99"); err != nil {
		t.Fatal(err)
	}
	n, _ = s.GetNote(n.ID)
	if UnlockToken([]byte("secret"), n) == tok1 {
		t.Fatal("changing passcode must invalidate unlock tokens")
	}

	if err := s.ClearPasscode(n.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	n, _ = s.GetNote(n.ID)
	if n.Protected() {
		t.Fatal("should be unprotected after clear")
	}
	if err := s.ClearPasscode(n.ID); err != nil {
		t.Fatalf("clear must be idempotent: %v", err)
	}
	if err := s.ClearPasscode("nope"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSitemapListing(t *testing.T) {
	s := newTestStore(t)
	pub := mustCreate(t, s, NoteInput{Title: "Public", HTML: "p"}, "")
	mustCreate(t, s, NoteInput{Title: "Hidden", HTML: "h", Noindex: true}, "")
	prot := mustCreate(t, s, NoteInput{Title: "Locked", HTML: "l"}, "")
	if err := s.SetPasscode(prot.ID, "some-pass-11"); err != nil {
		t.Fatal(err)
	}

	notes, err := s.ListSitemapNotes()
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Slug != pub.Slug {
		t.Fatalf("sitemap should only list the public note, got %d entries", len(notes))
	}
}

func TestTheme(t *testing.T) {
	s := newTestStore(t)
	empty, err := s.GetTheme()
	if err != nil || empty.UpdatedAt != 0 {
		t.Fatalf("fresh theme should be empty: %+v err=%v", empty, err)
	}
	if err := s.SetTheme(Theme{CSS: "body{}", JS: "//js", Head: "<link>"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTheme()
	if err != nil || got.CSS != "body{}" || got.JS != "//js" || got.Head != "<link>" || got.UpdatedAt == 0 {
		t.Fatalf("theme roundtrip failed: %+v err=%v", got, err)
	}
}

func TestCookieSecretStable(t *testing.T) {
	s := newTestStore(t)
	a, err := s.CookieSecret()
	if err != nil || len(a) != 32 {
		t.Fatalf("secret: %v len=%d", err, len(a))
	}
	b, _ := s.CookieSecret()
	if string(a) != string(b) {
		t.Fatal("cookie secret must be stable")
	}
}

func TestValidCustomSlug(t *testing.T) {
	valid := []string{"hello", "my-note-2", "x9", "b"}
	// "a", "api", "og" are reserved even when they match the regex.
	invalid := []string{"", "-lead", "trail-", "UPPER", "has space", "api", "a", "og", strings.Repeat("z", 81), "dots.not.ok"}
	for _, s := range valid {
		if !ValidCustomSlug(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range invalid {
		if ValidCustomSlug(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

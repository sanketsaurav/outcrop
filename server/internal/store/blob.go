package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var ErrHashMismatch = errors.New("uploaded bytes do not match the declared hash")

// Blobs stores attachment bytes on disk, content-addressed by SHA-256.
// Layout: <dir>/<hash[0:2]>/<hash>
type Blobs struct {
	dir string
}

func NewBlobs(dir string) (*Blobs, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating blob dir: %w", err)
	}
	return &Blobs{dir: dir}, nil
}

func (b *Blobs) path(hash string) string {
	return filepath.Join(b.dir, hash[:2], hash)
}

// Save streams r to disk, verifying its SHA-256 matches hash. Returns bytes written.
// Saving an already-present blob is a cheap no-op (the reader is drained).
func (b *Blobs) Save(hash string, r io.Reader) (int64, error) {
	if fi, err := os.Stat(b.path(hash)); err == nil {
		n, _ := io.Copy(io.Discard, r)
		_ = n
		return fi.Size(), nil
	}
	tmp, err := os.CreateTemp(b.dir, "upload-*")
	if err != nil {
		return 0, err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		return 0, err
	}
	if hex.EncodeToString(h.Sum(nil)) != hash {
		return 0, ErrHashMismatch
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(b.path(hash)), 0o755); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp.Name(), b.path(hash)); err != nil {
		return 0, err
	}
	return n, nil
}

func (b *Blobs) Open(hash string) (*os.File, error) {
	return os.Open(b.path(hash))
}

func (b *Blobs) Remove(hash string) error {
	err := os.Remove(b.path(hash))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

package store

import (
	"crypto/rand"
	"regexp"
)

// base58: no 0, O, I, l — slugs get read aloud and retyped.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func randBase58(n int) string {
	out := make([]byte, 0, n)
	buf := make([]byte, 64)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			panic("crypto/rand unavailable: " + err.Error())
		}
		for _, b := range buf {
			// Rejection sampling: 232 is the largest multiple of 58 ≤ 256.
			if b < 232 {
				out = append(out, base58Alphabet[int(b)%58])
				if len(out) == n {
					break
				}
			}
		}
	}
	return string(out)
}

// NewID returns a stable note identity (~128 bits).
func NewID() string { return randBase58(22) }

// NewSlug returns a public URL token (~58 bits, unguessable).
func NewSlug() string { return randBase58(10) }

var customSlugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// reservedSlugs are path segments the server routes itself.
var reservedSlugs = map[string]bool{
	"api": true, "a": true, "t": true, "og": true,
	"healthz": true, "robots.txt": true, "sitemap.xml": true, "favicon.ico": true,
}

func ValidCustomSlug(s string) bool {
	return s != "" && len(s) <= 80 && customSlugRe.MatchString(s) && !reservedSlugs[s]
}

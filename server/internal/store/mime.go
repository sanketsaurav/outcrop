package store

import "regexp"

// assetTypes is the extension whitelist and the authoritative MIME mapping —
// client-supplied content types are never trusted.
var assetTypes = map[string]string{
	"png":   "image/png",
	"jpg":   "image/jpeg",
	"jpeg":  "image/jpeg",
	"gif":   "image/gif",
	"webp":  "image/webp",
	"avif":  "image/avif",
	"svg":   "image/svg+xml",
	"mp3":   "audio/mpeg",
	"m4a":   "audio/mp4",
	"ogg":   "audio/ogg",
	"wav":   "audio/wav",
	"flac":  "audio/flac",
	"mp4":   "video/mp4",
	"webm":  "video/webm",
	"mov":   "video/quicktime",
	"pdf":   "application/pdf",
	"woff":  "font/woff",
	"woff2": "font/woff2",
}

// AssetMIME maps a whitelisted extension to its MIME type.
func AssetMIME(ext string) (string, bool) {
	m, ok := assetTypes[ext]
	return m, ok
}

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidAssetHash reports whether s is a lowercase hex SHA-256.
func ValidAssetHash(s string) bool { return hashRe.MatchString(s) }

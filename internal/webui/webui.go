// Package webui embeds the single self-contained page built by
// `bun run build:web` (scripts/build.ts writes dist/index.html).
package webui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
)

//go:embed all:dist
var dist embed.FS

// Page is the built page, or nil when the frontend was not built before
// compiling the plugin.
var Page, _ = dist.ReadFile("dist/index.html")

// CSP returns the Content-Security-Policy for page: only its own inline
// <script> and <style> (pinned by hash) may run, it may fetch only from its
// own origin, and only a same-origin page may frame it.
func CSP(page []byte) string {
	return "default-src 'none'; " +
		"script-src " + inlineHash(page, "<script>", "</script>") + "; " +
		"style-src " + inlineHash(page, "<style>", "</style>") + "; " +
		"connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"
}

func inlineHash(page []byte, open, close string) string {
	i := bytes.Index(page, []byte(open))
	if i < 0 {
		return "'none'"
	}
	body := page[i+len(open):]
	j := bytes.Index(body, []byte(close))
	if j < 0 {
		return "'none'"
	}
	sum := sha256.Sum256(body[:j])
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

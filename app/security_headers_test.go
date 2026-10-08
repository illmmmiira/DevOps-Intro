package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSecurityHeadersOnAllRoutes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.json")
	if err := os.WriteFile(p, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(p)
	if err != nil {
		t.Fatal(err)
	}
	h := buildHandler(NewServer(store).Routes())

	want := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"Cache-Control":                "no-store",
		"Cross-Origin-Embedder-Policy": "require-corp",
		"Content-Security-Policy":      "default-src 'none'; frame-ancestors 'none'",
	}
	for _, path := range []string{"/health", "/notes", "/metrics", "/does-not-exist"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		for name, v := range want {
			if got := rec.Header().Get(name); got != v {
				t.Errorf("%s: header %s = %q, want %q", path, name, got, v)
			}
		}
	}
}

package main

import "net/http"

// securityHeaders adds HTTP security headers to every response.
// QuickNotes is a JSON API, so the strictest policy is safe here.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Embedder-Policy", "require-corp")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// buildHandler wraps the whole router, so every route gets the headers.
func buildHandler(routes http.Handler) http.Handler {
	return securityHeaders(routes)
}

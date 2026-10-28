package server

import "net/http"

// contentSecurityPolicy tillåter bara egna filer: ingen inbäddad kod eller
// stil (inga 'unsafe-inline'/'unsafe-eval').
const contentSecurityPolicy = "default-src 'self'; img-src 'self'; style-src 'self'; script-src 'self'; " +
	"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		// "Min plats" kommer att behöva geolocation=(self).
		h.Set("Permissions-Policy", "geolocation=(), camera=(), microphone=(), payment=(), usb=()")
		next.ServeHTTP(w, r)
	})
}

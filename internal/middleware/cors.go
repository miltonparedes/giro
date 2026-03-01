package middleware

import "net/http"

// CORS returns a middleware that handles Cross-Origin Resource Sharing.
// Preflight OPTIONS requests receive the full set of permissive CORS headers
// and a 204 No Content response. All other requests get the minimal
// Access-Control-Allow-Origin and Access-Control-Allow-Credentials headers
// before being passed to the next handler.
//
// When Allow-Credentials is true the spec forbids the literal "*" for
// Allow-Origin, so we echo the request Origin header instead (falling back
// to "*" when no Origin is present). Vary: Origin is set so caches key on
// the origin.
func CORS() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				origin = "*"
			}

			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "*")
				w.Header().Set("Access-Control-Allow-Headers", "*")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.Header().Set("Vary", "Origin")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
			next.ServeHTTP(w, r)
		})
	}
}

package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

const Header = "X-Request-ID"

var trustedID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type contextKey struct{}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(Header)
		if !trustedID.MatchString(id) {
			var value [16]byte
			if _, err := rand.Read(value[:]); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			id = hex.EncodeToString(value[:])
		}
		w.Header().Set(Header, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, id)))
	})
}

func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

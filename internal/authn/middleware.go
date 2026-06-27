package authn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"zia/internal/repository"
)

type ctxKey string

const MerchantIDKey ctxKey = "merchant_id"

func Middleware(repo repository.MerchantRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			hash := sha256.Sum256([]byte(token))
			hashStr := hex.EncodeToString(hash[:])

			key, err := repo.GetAPIKeyByHash(r.Context(), hashStr)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), MerchantIDKey, key.MerchantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

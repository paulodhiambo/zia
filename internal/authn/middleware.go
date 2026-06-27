package authn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
				writeUnauthorized(w)
				return
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			hash := sha256.Sum256([]byte(token))
			hashStr := hex.EncodeToString(hash[:])

			key, err := repo.GetAPIKeyByHash(r.Context(), hashStr)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), MerchantIDKey, key.MerchantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]any{
		"statusCode":        "1002",
		"statusDescription": "Authentication failed",
		"messageCode":       "401",
		"messageDescription": "Invalid or missing credentials",
		"errorInfo":         map[string]string{"errorCode": "1002", "errorMessage": "unauthorized"},
		"primaryData":       nil,
	})
}

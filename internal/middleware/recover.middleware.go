package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/utils"
)

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error().Interface("panic", rec).Str("stack", string(debug.Stack())).Msg("panic recovered")
				utils.WriteError(w, nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

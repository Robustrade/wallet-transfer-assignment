package routes

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/handler"
)

func NewRouter(
	transferHandler *handler.TransferHandler,
) http.Handler {

	r := chi.NewRouter()

	//-------------------------------------------------------
	// Middlewares
	//-------------------------------------------------------

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	//-------------------------------------------------------
	// Health
	//-------------------------------------------------------

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	//-------------------------------------------------------
	// API
	//-------------------------------------------------------

	r.Route("/api/v1", func(r chi.Router) {

		r.Post(
			"/transfers",
			transferHandler.CreateTransfer,
		)

	})

	return r
}

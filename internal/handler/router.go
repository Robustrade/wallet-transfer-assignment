// Package handler is the transport layer: request decoding, validation
// mapping, and response encoding. It holds no business logic and no SQL.
package handler

import (
	"context"
	"net/http"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/service"
)

// TransferAPI is the service surface the handler depends on. Declared here so
// the handler can be tested with a stub.
type TransferAPI interface {
	CreateTransfer(ctx context.Context, in service.CreateTransferInput) (domain.Transfer, bool, error)
	GetTransfer(ctx context.Context, id string) (domain.Transfer, error)
}

// NewRouter wires the HTTP routes onto a stdlib ServeMux (Go 1.22+ method +
// path patterns).
func NewRouter(api TransferAPI) *http.ServeMux {
	h := &TransferHandler{api: api}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.Create)
	mux.HandleFunc("GET /transfers/{id}", h.Get)
	return mux
}

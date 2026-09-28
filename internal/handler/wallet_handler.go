package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

type walletHandler struct {
	svc WalletReader
}

// get handles GET /wallets/:id.
func (h *walletHandler) get(c *gin.Context) {
	w, err := h.svc.GetWallet(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, toWalletResponse(w))
}

// listTransfers handles GET /wallets/:id/transfers?limit=N.
//
// The handler only parses the query parameter (a transport concern). The
// allowed range (1..MaxListLimit) is a rule of the service, which rejects
// other values with a ValidationError (400).
func (h *walletHandler) listTransfers(c *gin.Context) {
	limit := service.DefaultListLimit
	if raw, ok := c.GetQuery("limit"); ok {
		n, err := strconv.Atoi(raw)
		if err != nil {
			respondError(c, &service.ValidationError{Field: "limit", Message: "must be an integer"})
			return
		}
		limit = n
	}

	transfers, err := h.svc.ListTransfers(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		respondError(c, err)
		return
	}
	resp := transferListResponse{Transfers: make([]transferResponse, 0, len(transfers))}
	for _, t := range transfers {
		resp.Transfers = append(resp.Transfers, toTransferResponse(t))
	}
	c.JSON(http.StatusOK, resp)
}

package handler

import (
	"errors"
	"net/http"
	"wallet-transfer/internal/wallet/domain"
	"wallet-transfer/internal/wallet/service"

	"github.com/gin-gonic/gin"
)

type Handler struct{ service *service.Service }

func New(s *service.Service) *Handler { return &Handler{service: s} }
func (h *Handler) Create(c *gin.Context) {
	var req domain.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	w, err := h.service.Create(c, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, w)
}

func (h *Handler) List(c *gin.Context) {
	wallets, err := h.service.List(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"wallets": wallets})
}

func (h *Handler) UpdateBalance(c *gin.Context) {
	var request domain.UpdateBalanceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	wallet, err := h.service.UpdateBalance(c, c.Param("id"), request.Balance)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrInvalidRequest) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, domain.ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, wallet)
}

func (h *Handler) Balance(c *gin.Context) {
	h.Get(c)
}

func (h *Handler) BalanceHistory(c *gin.Context) {
	history, err := h.service.BalanceHistory(c, c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrInvalidRequest) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, domain.ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"history": history})
}
func (h *Handler) Get(c *gin.Context) {
	w, err := h.service.Get(c, c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrInvalidRequest) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, domain.ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, w)
}
func (h *Handler) Transfers(c *gin.Context) {
	list, err := h.service.ListTransfers(c, c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrInvalidRequest) {
			status = http.StatusBadRequest
		}
		if errors.Is(err, domain.ErrNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"transfers": list})
}

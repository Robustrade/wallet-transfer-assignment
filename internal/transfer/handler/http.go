package handler

import (
	"errors"
	"net/http"
	"wallet-transfer/internal/transfer/domain"
	"wallet-transfer/internal/transfer/service"

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
	t, err := h.service.Create(c, req)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, domain.ErrInvalidRequest):
			status = http.StatusBadRequest
		case errors.Is(err, domain.ErrIdempotencyReuse), service.IsBusinessFailure(err):
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, t)
}
func (h *Handler) List(c *gin.Context) {
	list, err := h.service.ListAll(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"transfers": list})
}

func (h *Handler) Get(c *gin.Context) {
	transfer, err := h.service.Get(c, c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrInvalidRequest) || errors.Is(err, domain.ErrTransferNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, transfer)
}

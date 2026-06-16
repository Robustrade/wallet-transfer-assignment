package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/synchone/wallet-transfer-assignment/internal/repository"
)

type HealthPinger struct {
	repo *repository.Repository
}

func NewHealthPinger(repo *repository.Repository) *HealthPinger {
	return &HealthPinger{repo: repo}
}

func (h *HealthPinger) Ping(c *gin.Context) error {
	return h.repo.Ping(c.Request.Context())
}

type WalletBalanceService struct {
	repo *repository.Repository
}

func NewWalletBalanceService(repo *repository.Repository) *WalletBalanceService {
	return &WalletBalanceService{repo: repo}
}

func (s *WalletBalanceService) GetWalletBalance(c *gin.Context, walletID string) (int64, error) {
	return s.repo.GetWalletBalance(c.Request.Context(), walletID)
}

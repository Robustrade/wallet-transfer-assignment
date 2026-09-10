package helper

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/sushmitaw/wallet-transfer-assignment/internal/domain"
)

func RequestHash(req domain.CreateTransferRequest) string {
	value := fmt.Sprintf(
		"%s|%s|%d",
		req.FromWalletID.String(),
		req.ToWalletID.String(),
		req.Amount,
	)

	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}

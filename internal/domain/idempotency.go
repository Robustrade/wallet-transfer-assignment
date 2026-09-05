package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/uuid"
)

type IdempotencyRecord struct {
	Key          string
	Fingerprint  string
	TransferID   uuid.UUID
	ResponseBody json.RawMessage
}

func (r TransferRequest) Fingerprint() string {
	h := sha256.New()
	for _, part := range []string{r.FromWalletID, r.ToWalletID, strconv.FormatInt(int64(r.Amount), 10)} {
		fmt.Fprintf(h, "%d:%s", len(part), part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

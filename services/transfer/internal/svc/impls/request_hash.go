package svcimpl

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"strconv"

	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/request"
)

// hashRequest returns a stable SHA-256 hex digest of the canonical fields of
// a CreateTransferReq. It is the service layer's identity contract — what
// counts as "the same request" for idempotency purposes.
//
// Each variable-length field is length-prefixed (8-byte big-endian uint64) so
// that two distinct inputs cannot serialize to the same byte string. A naive
// delimiter (e.g. "|") would let `("a|b","c")` and `("a","b|c")` collide.
//
// The idempotencyKey is intentionally NOT part of the digest — it is the key
// under which the digest is stored, not part of the body identity.
func hashRequest(req request.CreateTransferReq) string {
	h := sha256.New()
	writeLP(h, []byte(req.FromWalletID))
	writeLP(h, []byte(req.ToWalletID))
	writeLP(h, []byte(strconv.FormatInt(req.Amount, 10)))
	return hex.EncodeToString(h.Sum(nil))
}

// writeLP writes a length-prefixed byte slice to h.
func writeLP(h hash.Hash, b []byte) {
	var lenBuf [8]byte
	binary.BigEndian.PutUint64(lenBuf[:], uint64(len(b)))
	h.Write(lenBuf[:])
	h.Write(b)
}

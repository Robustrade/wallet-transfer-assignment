package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
)

func validCommand() CreateTransferCommand {
	return CreateTransferCommand{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}
}

func TestCreateTransferCommand_Validate_Accepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		modify func(*CreateTransferCommand)
	}{
		{"typical request", func(*CreateTransferCommand) {}},
		{"key of 1 char", func(c *CreateTransferCommand) { c.IdempotencyKey = "k" }},
		{"key of 128 chars", func(c *CreateTransferCommand) { c.IdempotencyKey = strings.Repeat("k", 128) }},
		{"key using every allowed symbol", func(c *CreateTransferCommand) { c.IdempotencyKey = "Az09._:-" }},
		{"key that looks like a uuid", func(c *CreateTransferCommand) { c.IdempotencyKey = "0192a5b4-7c1e-7d3a-9f00-123456789abc" }},
		{"wallet ids of 1 char", func(c *CreateTransferCommand) { c.FromWalletID, c.ToWalletID = "a", "b" }},
		{"wallet id of 64 chars", func(c *CreateTransferCommand) { c.FromWalletID = strings.Repeat("w", 64) }},
		{"wallet ids differing only by case", func(c *CreateTransferCommand) { c.FromWalletID, c.ToWalletID = "W1", "w1" }},
		{"minimum amount", func(c *CreateTransferCommand) { c.Amount = 1 }},
		{"maximum amount", func(c *CreateTransferCommand) { c.Amount = MaxAmount }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := validCommand()
			tc.modify(&cmd)
			assert.NoError(t, cmd.Validate())
		})
	}
}

func TestCreateTransferCommand_Validate_Rejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		modify    func(*CreateTransferCommand)
		wantField string
	}{
		// idempotencyKey
		{"missing key", func(c *CreateTransferCommand) { c.IdempotencyKey = "" }, "idempotencyKey"},
		{"key of 129 chars", func(c *CreateTransferCommand) { c.IdempotencyKey = strings.Repeat("k", 129) }, "idempotencyKey"},
		{"key with inner space", func(c *CreateTransferCommand) { c.IdempotencyKey = "abc 123" }, "idempotencyKey"},
		{"key with leading space", func(c *CreateTransferCommand) { c.IdempotencyKey = " abc" }, "idempotencyKey"},
		{"key with trailing newline", func(c *CreateTransferCommand) { c.IdempotencyKey = "abc\n" }, "idempotencyKey"},
		{"key with tab", func(c *CreateTransferCommand) { c.IdempotencyKey = "a\tb" }, "idempotencyKey"},
		{"key only whitespace", func(c *CreateTransferCommand) { c.IdempotencyKey = "   " }, "idempotencyKey"},
		{"key with slash", func(c *CreateTransferCommand) { c.IdempotencyKey = "a/b" }, "idempotencyKey"},
		{"key with pipe", func(c *CreateTransferCommand) { c.IdempotencyKey = "a|b" }, "idempotencyKey"},
		{"key with non-ascii", func(c *CreateTransferCommand) { c.IdempotencyKey = "ключ" }, "idempotencyKey"},

		// fromWalletId
		{"missing from", func(c *CreateTransferCommand) { c.FromWalletID = "" }, "fromWalletId"},
		{"from of 65 chars", func(c *CreateTransferCommand) { c.FromWalletID = strings.Repeat("w", 65) }, "fromWalletId"},
		{"from with space", func(c *CreateTransferCommand) { c.FromWalletID = "wallet 1" }, "fromWalletId"},
		{"from with pipe", func(c *CreateTransferCommand) { c.FromWalletID = "a|b" }, "fromWalletId"},

		// toWalletId
		{"missing to", func(c *CreateTransferCommand) { c.ToWalletID = "" }, "toWalletId"},
		{"to of 65 chars", func(c *CreateTransferCommand) { c.ToWalletID = strings.Repeat("w", 65) }, "toWalletId"},
		{"to with non-ascii", func(c *CreateTransferCommand) { c.ToWalletID = "wället" }, "toWalletId"},

		// cross-field
		{"from equals to", func(c *CreateTransferCommand) { c.ToWalletID = c.FromWalletID }, "toWalletId"},

		// amount
		{"zero amount", func(c *CreateTransferCommand) { c.Amount = 0 }, "amount"},
		{"negative amount", func(c *CreateTransferCommand) { c.Amount = -1 }, "amount"},
		{"amount above maximum", func(c *CreateTransferCommand) { c.Amount = MaxAmount + 1 }, "amount"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := validCommand()
			tc.modify(&cmd)

			err := cmd.Validate()

			require.ErrorIs(t, err, domain.ErrValidation)
			var ve *ValidationError
			require.True(t, errors.As(err, &ve), "want *ValidationError, got %T", err)
			assert.Equal(t, tc.wantField, ve.Field)
			assert.NotEmpty(t, ve.Message)
		})
	}
}

func TestCreateTransferCommand_Validate_ReportsFirstFieldInFixedOrder(t *testing.T) {
	t.Parallel()

	// Everything is wrong; the key is checked first.
	err := CreateTransferCommand{}.Validate()

	var ve *ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Equal(t, "idempotencyKey", ve.Field)
}

func TestValidationError_Error(t *testing.T) {
	t.Parallel()

	err := &ValidationError{Field: "amount", Message: "must be at least 1"}
	assert.Equal(t, "amount: must be at least 1", err.Error())
}

func TestCreateTransferCommand_RequestHash(t *testing.T) {
	t.Parallel()

	t.Run("matches the golden vector", func(t *testing.T) {
		t.Parallel()
		// Computed independently:
		//   printf '%s' '8:wallet_1|8:wallet_2|100' | shasum -a 256
		// Stored hashes must stay comparable across deployments, so this
		// test fails if the encoding changes by accident.
		const want = "2051cec47767b839a8986a5c7d217ca4d604103c78ae86ffb23b8e4763a03336"
		assert.Equal(t, want, validCommand().RequestHash())
	})

	t.Run("is deterministic", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, validCommand().RequestHash(), validCommand().RequestHash())
	})

	t.Run("is 64 lowercase hex chars", func(t *testing.T) {
		t.Parallel()
		assert.Regexp(t, `^[0-9a-f]{64}$`, validCommand().RequestHash())
	})

	t.Run("ignores the idempotency key", func(t *testing.T) {
		t.Parallel()
		// The key is how we find the record; the hash describes the payload.
		other := validCommand()
		other.IdempotencyKey = "different"
		assert.Equal(t, validCommand().RequestHash(), other.RequestHash())
	})

	differsWhen := []struct {
		name   string
		modify func(*CreateTransferCommand)
	}{
		{"from differs", func(c *CreateTransferCommand) { c.FromWalletID = "wallet_3" }},
		{"to differs", func(c *CreateTransferCommand) { c.ToWalletID = "wallet_3" }},
		{"amount differs", func(c *CreateTransferCommand) { c.Amount = 101 }},
		{"from and to swapped", func(c *CreateTransferCommand) { c.FromWalletID, c.ToWalletID = c.ToWalletID, c.FromWalletID }},
		{"wallet id case differs", func(c *CreateTransferCommand) { c.FromWalletID = "WALLET_1" }},
	}
	for _, tc := range differsWhen {
		t.Run("differs when "+tc.name, func(t *testing.T) {
			t.Parallel()
			other := validCommand()
			tc.modify(&other)
			assert.NotEqual(t, validCommand().RequestHash(), other.RequestHash())
		})
	}

	t.Run("field boundaries are unambiguous", func(t *testing.T) {
		t.Parallel()
		// With a naive "from|to|amount" encoding these two would hash the
		// same. Validation rejects '|' in wallet ids, but the hash must not
		// depend on that.
		a := CreateTransferCommand{FromWalletID: "a|b", ToWalletID: "c", Amount: 1}
		b := CreateTransferCommand{FromWalletID: "a", ToWalletID: "b|c", Amount: 1}
		assert.NotEqual(t, a.RequestHash(), b.RequestHash())

		// The amount must not be able to absorb digits from a wallet id.
		c := CreateTransferCommand{FromWalletID: "a", ToWalletID: "b1", Amount: 2}
		d := CreateTransferCommand{FromWalletID: "a", ToWalletID: "b", Amount: 12}
		assert.NotEqual(t, c.RequestHash(), d.RequestHash())
	})
}

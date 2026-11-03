package wallet

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
)

var (
	ErrInvalidRequest   = errors.New("invalid request")
	ErrConflict         = errors.New("conflict")
	ErrNotFound         = errors.New("not found")
	ErrIdempotencyReuse = errors.New("idempotency key reused with different request")
)

type Money int64

func ParseMoney(raw []byte, allowZero bool) (Money, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" || strings.HasPrefix(value, `"`) {
		return 0, fmt.Errorf("%w: amount must be a positive number with at most two decimal places", ErrInvalidRequest)
	}
	if len(value) > 64 {
		return 0, fmt.Errorf("%w: amount is too large", ErrInvalidRequest)
	}

	exponent := 0
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		if strings.IndexAny(value[index+1:], "eE") >= 0 {
			return 0, invalidAmount()
		}
		parsed, err := strconv.Atoi(value[index+1:])
		if err != nil || parsed < -1000 || parsed > 1000 {
			return 0, invalidAmount()
		}
		exponent = parsed
		value = value[:index]
	}

	if strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		return 0, invalidAmount()
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, invalidAmount()
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" {
			return 0, invalidAmount()
		}
	}
	digits := parts[0] + fraction
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return 0, invalidAmount()
		}
	}

	minor, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return 0, invalidAmount()
	}
	power := exponent - len(fraction) + 2
	if power >= 0 {
		minor.Mul(minor, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(power)), nil))
	} else {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-power)), nil)
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(minor, divisor, remainder)
		if remainder.Sign() != 0 {
			return 0, invalidAmount()
		}
		minor = quotient
	}

	if !minor.IsInt64() || minor.Sign() < 0 {
		return 0, fmt.Errorf("%w: amount is too large", ErrInvalidRequest)
	}
	if minor.Sign() == 0 && !allowZero {
		return 0, invalidAmount()
	}
	return Money(minor.Int64()), nil
}

func invalidAmount() error {
	return fmt.Errorf("%w: amount must be a positive number with at most two decimal places", ErrInvalidRequest)
}

func (money *Money) UnmarshalJSON(data []byte) error {
	parsed, err := ParseMoney(data, true)
	if err != nil {
		return err
	}
	*money = parsed
	return nil
}

func (money Money) MarshalJSON() ([]byte, error) {
	minor := int64(money)
	if minor < 0 {
		return nil, fmt.Errorf("cannot serialize a negative money value")
	}
	whole, fraction := minor/100, minor%100
	if fraction == 0 {
		return []byte(strconv.FormatInt(whole, 10)), nil
	}
	if fraction%10 == 0 {
		return []byte(fmt.Sprintf("%d.%d", whole, fraction/10)), nil
	}
	return []byte(fmt.Sprintf("%d.%02d", whole, fraction)), nil
}

func normalizeText(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: %s must be a non-empty string", ErrInvalidRequest, field)
	}
	if len(value) > 255 {
		return "", fmt.Errorf("%w: %s must be at most 255 characters", ErrInvalidRequest, field)
	}
	return value, nil
}

type Wallet struct {
	WalletID  string `json:"walletId"`
	Balance   Money  `json:"balance"`
	CreatedAt string `json:"createdAt"`
}

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         Money  `json:"amount"`
}

type TransferResult struct {
	TransferID     string  `json:"transferId"`
	IdempotencyKey string  `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         Money   `json:"amount"`
	Status         string  `json:"status"`
	FailureCode    *string `json:"failureCode"`
	CreatedAt      string  `json:"createdAt"`
	requestHash    string
}

func decodeObject(data []byte, destination any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("%w: request body must be a JSON object", ErrInvalidRequest)
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidRequest, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: request body must contain a single JSON object", ErrInvalidRequest)
	}
	return nil
}

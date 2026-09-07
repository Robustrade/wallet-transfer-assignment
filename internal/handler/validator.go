package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"wallet-transfer-service/internal/model"
)

var walletIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// RegisterCustomValidators registers domain-specific validators with Gin
func RegisterCustomValidators() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		_ = v.RegisterValidation("wallet_id", func(fl validator.FieldLevel) bool {
			return walletIDRegex.MatchString(fl.Field().String())
		})

		_ = v.RegisterValidation("amount_positive", func(fl validator.FieldLevel) bool {
			val := fl.Field().String()
			minorUnits, err := model.ParseToMinorUnits(val)
			if err != nil {
				return false
			}
			return minorUnits > 0
		})

		_ = v.RegisterValidation("amount_non_negative", func(fl validator.FieldLevel) bool {
			val := fl.Field().String()
			minorUnits, err := model.ParseToMinorUnits(val)
			if err != nil {
				return false
			}
			return minorUnits >= 0
		})
	}
}

// HandleValidationError translates binding errors into a field-by-field JSON map
func HandleValidationError(c *gin.Context, err error) {
	slog.WarnContext(c.Request.Context(), "request validation failed", "path", c.Request.URL.Path, "error", err)
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		out := make(map[string]string)
		for _, fe := range ve {
			out[fe.Field()] = msgForTag(fe)
		}
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"details": out,
		})
		return
	}

	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
		"error":   "invalid_request",
		"details": err.Error(),
	})
}

func msgForTag(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "This field is required"
	case "wallet_id":
		return "Must contain only letters, numbers, and underscores (_)"
	case "amount_positive":
		return "Must be a valid positive amount greater than 0 with at most 2 decimal places (e.g. '10.50')"
	case "amount_non_negative":
		return "Must be a valid non-negative amount with at most 2 decimal places (e.g. '0.00' or '100.00')"
	case "alphanum":
		return "Must contain only letters and numbers (no spaces or special characters)"
	case "min":
		return fmt.Sprintf("Must be at least %s characters long", fe.Param())
	case "max":
		return fmt.Sprintf("Must be at most %s characters long", fe.Param())
	case "gt":
		return fmt.Sprintf("Must be strictly greater than %s", fe.Param())
	case "gte":
		return fmt.Sprintf("Must be greater than or equal to %s", fe.Param())
	case "nefield":
		return fmt.Sprintf("Cannot be identical to %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("Must be one of: %s", fe.Param())
	default:
		return "Invalid value"
	}
}

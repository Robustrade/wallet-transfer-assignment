package validate

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

var v = validator.New(validator.WithRequiredStructEnabled())

// Validate validates a struct using field-level validate tags.
// Returns a user-friendly error message string, or empty string if valid.
func Validate(body interface{}) string {
	err := v.Struct(body)
	if err != nil {
		if _, ok := err.(*validator.InvalidValidationError); ok {
			return "invalid input"
		}
		for _, fe := range err.(validator.ValidationErrors) {
			return validationMessage(fe)
		}
	}
	return ""
}

func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", fe.Field())
	case "email":
		return fmt.Sprintf("%s must be a valid email address", fe.Field())
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", fe.Field(), fe.Param())
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", fe.Field(), fe.Param())
	default:
		return fmt.Sprintf("%s is invalid", fe.Field())
	}
}

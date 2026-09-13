package utils

import "github.com/go-playground/validator/v10"

var validate = validator.New()

func ValidateRequest(req any) error {
	return validate.Struct(req)
}

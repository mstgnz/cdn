package validator

import (
	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// ValidateStruct validates a struct using validator tags
func ValidateStruct(s any) error {
	return validate.Struct(s)
}

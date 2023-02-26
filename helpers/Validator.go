package helpers

import (
	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

type ValidationError struct {
	FailedField string
	Tag         string
	Value       string
}

func ValidateStruct[T any](data T) []*ValidationError {
	var errors []*ValidationError
	err := validate.Struct(data)
	if err != nil {
		for _, err := range err.(validator.ValidationErrors) {
			var el ValidationError
			el.FailedField = err.Field()
			el.Tag = err.Tag()
			el.Value = err.Param()
			errors = append(errors, &el)
		}
	}
	return errors
}

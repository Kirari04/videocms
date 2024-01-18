package helpers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
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

func Validate[ValidationModel any](c echo.Context, validationModel *ValidationModel) (int, error) {
	err := c.Bind(validationModel)
	if err != nil {
		return http.StatusBadRequest, errors.New("malformated request")
	}

	if errors := ValidateStruct(validationModel); len(errors) > 0 {
		return http.StatusBadRequest, fmt.Errorf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value)
	}

	return 0, nil
}

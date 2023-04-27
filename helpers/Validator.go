package helpers

import (
	"errors"
	"fmt"
	"log"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
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

func Validate[ValidationModel any](ctx *fiber.Ctx, validationModel *ValidationModel, parser string) (int, error) {
	switch parser {
	case "body":
		ctx.BodyParser(validationModel)
	case "query":
		ctx.QueryParser(validationModel)
	case "param":
		ctx.ParamsParser(validationModel)
	default:
		log.Printf("Incorrect validation parser was set: %s", parser)
		return fiber.StatusInternalServerError, errors.New(fiber.ErrInternalServerError.Message)
	}

	if errors := ValidateStruct(validationModel); len(errors) > 0 {
		return fiber.StatusBadRequest, fmt.Errorf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value)
	}

	return 0, nil
}

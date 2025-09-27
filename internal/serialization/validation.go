package serialization

import (
	"github.com/go-playground/validator/v10"
	gperr "github.com/yusing/goutils/errs"
)

var validate = validator.New()

var ErrValidationError = gperr.New("validation error")

type CustomValidator interface {
	Validate() gperr.Error
}

func Validator() *validator.Validate {
	return validate
}

func MustRegisterValidation(tag string, fn validator.Func) {
	err := validate.RegisterValidation(tag, fn)
	if err != nil {
		panic(err)
	}
}

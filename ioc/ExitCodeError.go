package ioc

import (
	"github.com/go-external-config/go/util/err"
)

type ExitCodeError struct {
	*err.AbstractError
	code int
}

func NewExitCodeError(code int, message string) *ExitCodeError {
	return &ExitCodeError{
		AbstractError: err.NewAbstractError(message, nil, nil),
		code:          code,
	}
}

func NewExitCodeErrorWithCause(code int, message string, cause any) *ExitCodeError {
	return &ExitCodeError{
		AbstractError: err.NewAbstractError(message, cause, nil),
		code:          code,
	}
}

func NewExitCodeErrorWithStack(code int, message string, cause any, stackTrace []uintptr) *ExitCodeError {
	return &ExitCodeError{
		AbstractError: err.NewAbstractError(message, cause, stackTrace),
		code:          code,
	}
}

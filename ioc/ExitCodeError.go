package ioc

import "github.com/go-errr/go/err"

type ExitCodeError struct {
	*err.AbstractError
	code int
}

func NewExitCodeError(code int, message string) *ExitCodeError {
	return &ExitCodeError{
		AbstractError: err.NewAbstractError(message),
		code:          code,
	}
}

func NewExitCodeErrorFrom(code int, message string, cause any) *ExitCodeError {
	return &ExitCodeError{
		AbstractError: err.NewAbstractErrorFrom(message, cause),
		code:          code,
	}
}

func NewExitCodeErrorWith(code int, message string, cause any, stackTrace []uintptr) *ExitCodeError {
	return &ExitCodeError{
		AbstractError: err.NewAbstractErrorWith(message, cause, stackTrace),
		code:          code,
	}
}

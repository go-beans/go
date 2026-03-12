package ioc

import "github.com/go-errr/go/err"

type ExitCodeError struct {
	*err.AbstractError
	code int
}

func NewExitCodeError(code int, message string) *ExitCodeError {
	return &ExitCodeError{
		AbstractError: err.NewAbstractError(message, nil, err.StackTrace(1)),
		code:          code,
	}
}

func NewExitCodeErrorFrom(code int, message string, cause any) *ExitCodeError {
	return &ExitCodeError{
		AbstractError: err.NewAbstractError(message, cause, err.StackTrace(1)),
		code:          code,
	}
}

func NewExitCodeErrorWith(code int, message string, cause any, stackTrace []uintptr) *ExitCodeError {
	return &ExitCodeError{
		AbstractError: err.NewAbstractError(message, cause, stackTrace),
		code:          code,
	}
}

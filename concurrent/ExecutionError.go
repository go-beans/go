package concurrent

import "github.com/go-external-config/go/util/err"

type ExecutionError struct {
	*err.AbstractError
}

func NewExecutionError(message string, cause any, stackTrace []uintptr) *ExecutionError {
	return &ExecutionError{
		AbstractError: err.NewAbstractError(message, cause, stackTrace),
	}
}

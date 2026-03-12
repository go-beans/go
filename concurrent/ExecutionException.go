package concurrent

import "github.com/go-errr/go/err"

type ExecutionException struct {
	*err.AbstractException
}

func NewExecutionException(message string, cause any, stackTrace []uintptr) *ExecutionException {
	return &ExecutionException{
		AbstractException: err.NewAbstractException(message, cause, stackTrace),
	}
}

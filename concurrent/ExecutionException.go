package concurrent

import (
	"fmt"

	"github.com/go-errr/go/err"
)

type ExecutionException struct {
	err.AbstractException
}

func NewExecutionException(message string) *ExecutionException {
	return &ExecutionException{
		AbstractException: *err.NewAbstractException(message, nil, err.StackTrace(1)),
	}
}

func NewExecutionExceptionFrom(message string, cause any) *ExecutionException {
	return &ExecutionException{
		AbstractException: *err.NewAbstractException(message, cause, err.StackTrace(1)),
	}
}

func NewExecutionExceptionWith(message string, cause any, stackTrace []uintptr) *ExecutionException {
	return &ExecutionException{
		AbstractException: *err.NewAbstractException(message, cause, stackTrace),
	}
}

func (this *ExecutionException) Format(s fmt.State, verb rune) {
	this.DefaultFormat(s, verb, this)
}

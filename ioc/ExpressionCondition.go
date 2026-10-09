package ioc

import (
	"fmt"

	"github.com/go-external-config/go/env"
)

type ExpressionCondition struct {
	expression string
}

func NewExpressionCondition(expression string) *ExpressionCondition {
	return &ExpressionCondition{expression: expression}
}

func (this *ExpressionCondition) Matches() bool {
	return env.Value[bool](fmt.Sprintf("#{%s}", this.expression))
}

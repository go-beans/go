package ioc_test

import (
	"fmt"
	"testing"

	"github.com/go-beans/go/ioc"
	"github.com/go-external-config/go/env"
	"github.com/stretchr/testify/require"
)

func init() {
	ioc.Bean[CalculatorImpl]().Factory(NewCalculatorImpl).PostConstruct((*CalculatorImpl).PostConstruct).Register()
	ioc.Bean[AddOperation]().Name("addOperation").Factory(NewAddOperation).Register()
	ioc.Bean[SubtractOperation]().Name("subtractOperation").Factory(NewSubtractOperation).Register()
	ioc.Bean[MultiplyOperation]().Name("multiplyOperation").Factory(NewMultiplyOperation).Register()
	ioc.Bean[DivideOperation]().Name("divideOperation").Factory(NewDivideOperation).Register()
}

func Test_Ioc(t *testing.T) {
	env.SetActiveProfiles("test")
	t.Run("should decode property", func(t *testing.T) {
		calculator := ioc.Inject[*CalculatorImpl]()
		consumer := NewConsumer()
		require.Equal(t, "PostConstruct: 4", calculator().LastOperation())
		require.Equal(t, 101, consumer.compute(100, 2))
		require.Equal(t, "divide", calculator().LastOperation())
	})
}

type Calculator interface {
	Add(a, b int) int
	Subtract(a, b int) int
	Multiply(a, b int) int
	Divide(a, b int) int
}

type CalculatorImpl struct {
	addOperation      func() Operation
	subtractOperation func() Operation
	multiplyOperation func() Operation
	divideOperation   func() Operation
	lastOperation     string
}

func NewCalculatorImpl() *CalculatorImpl {
	return &CalculatorImpl{
		addOperation:      ioc.Inject[Operation]("addOperation"),
		subtractOperation: ioc.Inject[Operation]("subtractOperation"),
		multiplyOperation: ioc.Inject[Operation]("multiplyOperation"),
		divideOperation:   ioc.Inject[Operation]("divideOperation"),
	}
}

func (c *CalculatorImpl) Add(a, b int) int {
	return c.addOperation().Calculate(a, b)
}
func (c *CalculatorImpl) Subtract(a, b int) int {
	return c.subtractOperation().Calculate(a, b)
}
func (c *CalculatorImpl) Multiply(a, b int) int {
	return c.multiplyOperation().Calculate(a, b)
}
func (c *CalculatorImpl) Divide(a, b int) int {
	return c.divideOperation().Calculate(a, b)
}
func (c *CalculatorImpl) LastOperation() string {
	return c.lastOperation
}
func (c *CalculatorImpl) SetLastOperation(lastOperation string) {
	c.lastOperation = lastOperation
}
func (c *CalculatorImpl) PostConstruct() {
	c.lastOperation = fmt.Sprintf("PostConstruct: %v", c.addOperation().Calculate(2, 2))
}

type Operation interface {
	Calculate(a, b int) int
}

type AddOperation struct {
	calculator func() *CalculatorImpl
}

func NewAddOperation() *AddOperation {
	return &AddOperation{calculator: ioc.Inject[*CalculatorImpl]()}
}
func (o *AddOperation) Calculate(a, b int) int {
	o.calculator().SetLastOperation("add")
	return a + b
}

type SubtractOperation struct {
	calculator func() *CalculatorImpl
}

func NewSubtractOperation() *SubtractOperation {
	return &SubtractOperation{calculator: ioc.Inject[*CalculatorImpl]()}
}
func (o *SubtractOperation) Calculate(a, b int) int {
	o.calculator().SetLastOperation("subtract")
	return a - b
}

type MultiplyOperation struct {
	calculator func() *CalculatorImpl
}

func NewMultiplyOperation() *MultiplyOperation {
	return &MultiplyOperation{calculator: ioc.Inject[*CalculatorImpl]()}
}
func (o *MultiplyOperation) Calculate(a, b int) int {
	o.calculator().SetLastOperation("multiply")
	return a * b
}

type DivideOperation struct {
	calculator func() *CalculatorImpl
}

func NewDivideOperation() *DivideOperation {
	return &DivideOperation{calculator: ioc.Inject[*CalculatorImpl]()}
}
func (o *DivideOperation) Calculate(a, b int) int {
	o.calculator().SetLastOperation("divide")
	return a / b
}

type Consumer struct {
	calculator func() Calculator
}

func NewConsumer() *Consumer {
	return &Consumer{
		calculator: ioc.Inject[Calculator]()}
}
func (c Consumer) compute(a, b int) int {
	x := c.calculator().Add(a, b)
	x = c.calculator().Multiply(x, b)
	x = c.calculator().Subtract(x, b)
	x = c.calculator().Divide(x, b)
	return x
}

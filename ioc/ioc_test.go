package ioc_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/go-beans/go/ioc"
	"github.com/go-external-config/go/env"
	"github.com/go-external-config/go/util"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	fmt.Println("Before all")
	env.SetActiveProfiles("test")

	ioc.Bean[Counter]().Name("singletonCounter").Factory(NewCounter).Register()
	ioc.Bean[Counter]().Scope("prototype").Name("prototypeCounter").Factory(NewCounter).Register()

	ioc.Bean[CalculatorImpl]().Primary().Profile("test").Factory(NewCalculatorImpl).PostConstruct((*CalculatorImpl).PostConstruct).PreDestroy((*CalculatorImpl).PreDestroy).Register()
	ioc.Bean[CalculatorImpl]().Factory(NewCalculatorImpl).PostConstruct((*CalculatorImpl).PostConstruct).Register()
	ioc.Bean[AddOperation]().Name("addOperation").Factory(NewAddOperation).Register()
	ioc.Bean[SubtractOperation]().Name("subtractOperation").Factory(NewSubtractOperation).Register()
	ioc.Bean[MultiplyOperation]().Name("multiplyOperation").Factory(NewMultiplyOperation).Register()
	ioc.Bean[DivideOperation]().Name("divideOperation").Factory(NewDivideOperation).PreDestroy((*DivideOperation).PreDestroy).Register()

	ioc.Bean[MockCalculator]().Primary().Profile("!test").Factory(func() *MockCalculator {
		mockCalculator := new(MockCalculator)
		mockCalculator.On("Add", 100, 2).Return(102)
		mockCalculator.On("Multiply", 102, 2).Return(204)
		mockCalculator.On("Subtract", 204, 2).Return(202)
		mockCalculator.On("Divide", 202, 2).Return(101)
		mockCalculator.On("LastOperation").Return("TBD")
		return mockCalculator
	}).Register()

	ioc.Bean[map[string]string]().Name("preinitializedMap").Factory(func() *map[string]string {
		m := make(map[string]string)
		m["key"] = "value"
		return &m
	}).Register()

	ioc.Bean[http.Client]().Factory(func() *http.Client {
		return &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}
	}).Register()

	m.Run()

	fmt.Println("After all")
	ioc.ApplicationContextInstance().Close()
}

func Test_Ioc(t *testing.T) {
	t.Run("general examples", func(t *testing.T) {
		preinitializedMap := ioc.Inject[*map[string]string]("preinitializedMap")
		require.Equal(t, "value", (*preinitializedMap())["key"])

		singletonCounter1 := ioc.Inject[*Counter]("singletonCounter")
		singletonCounter2 := ioc.Inject[*Counter]("singletonCounter")
		prototypeCounter1 := ioc.Inject[*Counter]("prototypeCounter")
		prototypeCounter2 := ioc.Inject[*Counter]("prototypeCounter")
		singletonCounter1().count++
		singletonCounter2().count++
		prototypeCounter1().count++
		prototypeCounter2().count++
		require.Equal(t, 2, singletonCounter1().count)
		require.Equal(t, 2, singletonCounter2().count)
		require.Equal(t, 1, prototypeCounter1().count)
		require.Equal(t, 1, prototypeCounter2().count)

		httpClient := ioc.Inject[*http.Client]()
		require.Equal(t, "200 OK", util.OptionalOfCommaErr(httpClient().Get("http://example.com")).Value().Status)
	})
}

func Test_IocCalculator(t *testing.T) {
	t.Run("calculator singleton share data, post construct executed", func(t *testing.T) {
		consumer := NewConsumer()
		calculator := ioc.Inject[Calculator]()

		require.Equal(t, "PostConstruct: 4", calculator().LastOperation())
		require.Equal(t, 101, consumer.compute(100, 2))
		require.Equal(t, "divide", calculator().LastOperation())
	})
}

func Test_IocCalculatorMock(t *testing.T) {
	t.Run("mock any bean for test", func(t *testing.T) {
		t.Skip("Switch MockCalculator profile to 'test', switch CalculatorImpl profile to '!test', disable Test_IocCalculator, enable this test")
		mockCalculator := ioc.Inject[*MockCalculator]()()
		consumer := NewConsumer()
		calculator := ioc.Inject[Calculator]()

		clearMethodExpectations(&mockCalculator.Mock, "LastOperation")
		mockCalculator.On("LastOperation").Return("PostConstruct: 4")
		require.Equal(t, "PostConstruct: 4", calculator().LastOperation())
		require.Equal(t, 101, consumer.compute(100, 2))
		clearMethodExpectations(&mockCalculator.Mock, "LastOperation")
		mockCalculator.On("LastOperation").Return("divide")
		require.Equal(t, "divide", calculator().LastOperation())
	})
}

type Counter struct {
	count int
}

func NewCounter() *Counter {
	return &Counter{}
}

type Calculator interface {
	Add(a, b int) int
	Subtract(a, b int) int
	Multiply(a, b int) int
	Divide(a, b int) int
	LastOperation() string
	SetLastOperation(string)
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
func (c *CalculatorImpl) PreDestroy() {
	c.lastOperation = "PreDestroy"
}

type Operation interface {
	Calculate(a, b int) int
}

type AddOperation struct {
	calculator func() Calculator
}

func NewAddOperation() *AddOperation {
	return &AddOperation{calculator: ioc.Inject[Calculator]()}
}
func (o *AddOperation) Calculate(a, b int) int {
	o.calculator().SetLastOperation("add")
	return a + b
}

type SubtractOperation struct {
	calculator func() Calculator
}

func NewSubtractOperation() *SubtractOperation {
	return &SubtractOperation{calculator: ioc.Inject[Calculator]()}
}
func (o *SubtractOperation) Calculate(a, b int) int {
	o.calculator().SetLastOperation("subtract")
	return a - b
}

type MultiplyOperation struct {
	calculator func() Calculator
}

func NewMultiplyOperation() *MultiplyOperation {
	return &MultiplyOperation{calculator: ioc.Inject[Calculator]()}
}
func (o *MultiplyOperation) Calculate(a, b int) int {
	o.calculator().SetLastOperation("multiply")
	return a * b
}

type DivideOperation struct {
	calculator func() Calculator
}

func NewDivideOperation() *DivideOperation {
	return &DivideOperation{calculator: ioc.Inject[Calculator]()}
}
func (o *DivideOperation) Calculate(a, b int) int {
	o.calculator().SetLastOperation("divide")
	return a / b
}
func (o *DivideOperation) PreDestroy() {
	panic("failed")
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

type MockCalculator struct {
	mock.Mock
}

func (m *MockCalculator) Add(a, b int) int {
	args := m.Called(a, b)
	return args.Int(0)
}
func (m *MockCalculator) Subtract(a, b int) int {
	args := m.Called(a, b)
	return args.Int(0)
}
func (m *MockCalculator) Multiply(a, b int) int {
	args := m.Called(a, b)
	return args.Int(0)
}
func (m *MockCalculator) Divide(a, b int) int {
	args := m.Called(a, b)
	return args.Int(0)
}
func (m *MockCalculator) LastOperation() string {
	args := m.Called()
	return args.String(0)
}
func (m *MockCalculator) SetLastOperation(o string) {
	m.Called(o)
}

func clearMethodExpectations(m *mock.Mock, methodName string) {
	filtered := m.ExpectedCalls[:0] // reuse underlying array

	for _, call := range m.ExpectedCalls {
		if call.Method != methodName {
			filtered = append(filtered, call)
		}
	}

	m.ExpectedCalls = filtered
}

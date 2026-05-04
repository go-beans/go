package ioc_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/go-beans/go/ioc"
	"github.com/go-external-config/go/env"
	"github.com/go-external-config/go/util/optional"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	fmt.Println("Before all")
	env.SetActiveProfiles("test")

	ioc.Bean[*Counter]().Name("singletonCounter", "counter").Factory(NewCounter).Register()
	ioc.Bean[*Counter]().Scope("prototype").Name("prototypeCounter").Factory(NewCounter).Register()

	ioc.Bean[*CalculatorImpl]().Primary().Profile("test").Factory(NewCalculatorImpl).PostConstruct((*CalculatorImpl).PostConstruct).PreDestroy((*CalculatorImpl).PreDestroy).Register()
	ioc.Bean[*CalculatorImpl]().Factory(NewCalculatorImpl).PostConstruct((*CalculatorImpl).PostConstruct).Register()
	ioc.Bean[*AddOperation]().Name("addOperation").Factory(NewAddOperation).Register()
	ioc.Bean[*SubtractOperation]().Name("subtractOperation").Factory(NewSubtractOperation).Register()
	ioc.Bean[*MultiplyOperation]().Name("multiplyOperation").Factory(NewMultiplyOperation).Register()
	ioc.Bean[*DivideOperation]().Name("divideOperation").Factory(NewDivideOperation).PreDestroy((*DivideOperation).PreDestroy).Register()

	ioc.Bean[*MockCalculator]().Primary().Profile("!test").Factory(func() *MockCalculator {
		mockCalculator := new(MockCalculator)
		mockCalculator.On("Add", 100, 2).Return(102)
		mockCalculator.On("Multiply", 102, 2).Return(204)
		mockCalculator.On("Subtract", 204, 2).Return(202)
		mockCalculator.On("Divide", 202, 2).Return(101)
		mockCalculator.On("LastOperation").Return("TBD")
		return mockCalculator
	}).Register()

	ioc.Bean[*map[string]string]().Name("preinitializedMap").Factory(func() *map[string]string {
		m := make(map[string]string)
		m["key"] = "value"
		return &m
	}).Register()

	ioc.Bean[*http.Client]().Factory(func() *http.Client {
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
	ioc.Close()
}

func Test_Ioc(t *testing.T) {
	t.Run("general examples", func(t *testing.T) {
		preinitializedMap := ioc.Resolve[*map[string]string]("preinitializedMap")
		require.Equal(t, "value", (*preinitializedMap())["key"])

		singletonCounter1 := ioc.Resolve[*Counter]("singletonCounter")
		singletonCounter2 := ioc.Resolve[*Counter]("singletonCounter")
		require.Equal(t, 0, singletonCounter1().count)
		require.Equal(t, 0, singletonCounter2().count)
		singletonCounter1().count++
		singletonCounter2().count++
		require.Equal(t, 2, singletonCounter1().count)
		require.Equal(t, 2, singletonCounter2().count)

		prototypeCounter1 := ioc.Resolve[*Counter]("prototypeCounter")
		prototypeCounter2 := ioc.Resolve[*Counter]("prototypeCounter")
		require.Equal(t, 0, prototypeCounter1().count)
		require.Equal(t, 0, prototypeCounter2().count)
		prototypeCounter1().count++
		prototypeCounter2().count++
		require.Equal(t, 1, prototypeCounter1().count)
		require.Equal(t, 1, prototypeCounter2().count)

		counterAlias := ioc.Resolve[*Counter]("counter")
		require.Equal(t, 2, counterAlias().count)

		httpClient := ioc.Resolve[*http.Client]()
		require.Equal(t, "200 OK", optional.OfCommaErr(httpClient().Get("http://example.com")).Value().Status)
	})
}

func Test_IocCalculator(t *testing.T) {
	t.Run("calculator singleton share data, post construct executed", func(t *testing.T) {
		consumer := NewConsumer()
		ioc.InjectBeans(consumer)
		calculator := ioc.Resolve[Calculator]()

		require.Equal(t, "PostConstruct: 4", calculator().LastOperation())
		require.Equal(t, 101, consumer.compute(100, 2))
		require.Equal(t, "divide", calculator().LastOperation())
	})
}

func Test_IocCalculatorMock(t *testing.T) {
	t.Run("mock any bean for test", func(t *testing.T) {
		t.Skip("Switch MockCalculator profile to 'test', switch CalculatorImpl profile to '!test', disable Test_IocCalculator, enable this test")
		mockCalculator := ioc.Resolve[*MockCalculator]()()
		consumer := NewConsumer()
		calculator := ioc.Resolve[Calculator]()

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
	addOperation      Operation `inject:"addOperation"`
	subtractOperation Operation `inject:"subtractOperation"`
	multiplyOperation Operation `inject:"multiplyOperation"`
	divideOperation   Operation `inject:"divideOperation"`
	lastOperation     string
}

func NewCalculatorImpl() *CalculatorImpl {
	return &CalculatorImpl{}
}

func (this *CalculatorImpl) Add(a, b int) int {
	return this.addOperation.Calculate(a, b)
}
func (this *CalculatorImpl) Subtract(a, b int) int {
	return this.subtractOperation.Calculate(a, b)
}
func (this *CalculatorImpl) Multiply(a, b int) int {
	return this.multiplyOperation.Calculate(a, b)
}
func (this *CalculatorImpl) Divide(a, b int) int {
	return this.divideOperation.Calculate(a, b)
}
func (this *CalculatorImpl) LastOperation() string {
	return this.lastOperation
}
func (this *CalculatorImpl) SetLastOperation(lastOperation string) {
	this.lastOperation = lastOperation
}
func (this *CalculatorImpl) PostConstruct() {
	this.lastOperation = fmt.Sprintf("PostConstruct: %v", this.addOperation.Calculate(2, 2))
}
func (this *CalculatorImpl) PreDestroy() {
	this.lastOperation = "PreDestroy"
}

type Operation interface {
	Calculate(a, b int) int
}

type AddOperation struct {
	calculator Calculator `inject:""`
}

func NewAddOperation() *AddOperation {
	return &AddOperation{}
}
func (this *AddOperation) Calculate(a, b int) int {
	this.calculator.SetLastOperation("add")
	return a + b
}

type SubtractOperation struct {
	calculator Calculator `inject:""`
}

func NewSubtractOperation() *SubtractOperation {
	return &SubtractOperation{}
}
func (this *SubtractOperation) Calculate(a, b int) int {
	this.calculator.SetLastOperation("subtract")
	return a - b
}

type MultiplyOperation struct {
	calculator Calculator `inject:""`
}

func NewMultiplyOperation() *MultiplyOperation {
	return &MultiplyOperation{}
}
func (this *MultiplyOperation) Calculate(a, b int) int {
	this.calculator.SetLastOperation("multiply")
	return a * b
}

type DivideOperation struct {
	calculator Calculator `inject:""`
}

func NewDivideOperation() *DivideOperation {
	return &DivideOperation{}
}
func (this *DivideOperation) Calculate(a, b int) int {
	this.calculator.SetLastOperation("divide")
	return a / b
}
func (this *DivideOperation) PreDestroy() {
	panic("failed")
}

type Consumer struct {
	calculator Calculator `inject:""`
}

func NewConsumer() *Consumer {
	return &Consumer{}
}
func (this Consumer) compute(a, b int) int {
	x := this.calculator.Add(a, b)
	x = this.calculator.Multiply(x, b)
	x = this.calculator.Subtract(x, b)
	x = this.calculator.Divide(x, b)
	return x
}

type MockCalculator struct {
	mock.Mock
}

func (this *MockCalculator) Add(a, b int) int {
	args := this.Called(a, b)
	return args.Int(0)
}
func (this *MockCalculator) Subtract(a, b int) int {
	args := this.Called(a, b)
	return args.Int(0)
}
func (this *MockCalculator) Multiply(a, b int) int {
	args := this.Called(a, b)
	return args.Int(0)
}
func (this *MockCalculator) Divide(a, b int) int {
	args := this.Called(a, b)
	return args.Int(0)
}
func (this *MockCalculator) LastOperation() string {
	args := this.Called()
	return args.String(0)
}
func (this *MockCalculator) SetLastOperation(o string) {
	this.Called(o)
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

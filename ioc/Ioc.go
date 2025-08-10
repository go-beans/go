package ioc

import (
	"context"
	"sync"

	"github.com/go-external-config/go/lang"
)

var applicationContext *ApplicationContext

// Inject bean by type and (optionaly) name
func Inject[T any](name ...string) func() T {
	lang.AssertState(len(name) <= 1, "Optional bean name expected")
	if len(name) == 1 {
		return newInjectQualifier[T]().Name(name[0]).resolve()
	}
	return newInjectQualifier[T]().resolve()
}

// Register bean
func Bean[T any]() *BeanDefinitionImpl[T] {
	return newBeanDefinition[T]()
}

func GracefulShutdown(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		ApplicationContextInstance().Close()
	}()
}

func ApplicationContextInstance() *ApplicationContext {
	if applicationContext == nil {
		applicationContext = newApplicationContext()
	}
	return applicationContext
}

package ioc

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

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

func ShutdownWaitGroup() *sync.WaitGroup {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	var shutdown sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	GracefulShutdown(ctx, &shutdown)
	go func() {
		<-sig
		cancel()
	}()
	return &shutdown
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

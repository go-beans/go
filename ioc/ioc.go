package ioc

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-external-config/go/lang"
)

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

// The returned context's Done channel is closed when AppcilationContext instance is closed and before beans destruction
//
//	for {
//		select {
//		case <-reqContext.Done():
//			return
//		case <-ioc.Context().Done():
//			return
//		case msg := <-ch:
//			fmt.Println(msg)
//		}
//	}
func Context() context.Context {
	return applicationContextInstance().context
}

// Refresh application context: instantiate and initialize all non-lazy singeton beans. Omit if you want beans to be `lazy` by default
func Refresh() {
	applicationContextInstance().Refresh()
}

// To be used in main to defer resources cleanup
//
//	defer ioc.Close()
func Close() {
	applicationContextInstance().Close()
}

func AwaitTermination() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

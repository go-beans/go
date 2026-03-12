// 0. Bean registration
//
// 1. Bean instantiation
//
// 2. Dependency injection
//
// 3. Aware callbacks
//
// 4. PostConstruct
//
// 5. InitializingBean.AfterPropertiesSet()
//
// 6. Lfecycle.Start()
//
// 7. ContextRefreshedEvent
//
// 8. ApplicationRunner.Run()
//
// 9. ApplicationReadyEvent / ApplicationFailedEvent
//
// # APPLICATION RUNNING
//
// 10. ContextClosedEvent
//
// 11. Lfecycle.Stop()
//
// 12. PreDestroy
//
// 13. DisposableBean.Destroy()
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

// Refresh application context: instantiate and initialize all non-lazy singeton beans
func Refresh() {
	applicationContextInstance().Refresh()
}

// Refresh application context
// Execute ApplicationRunner(s)
func Run() {
	applicationContextInstance().Run()
}

// To be used in main to defer resources cleanup
//
//	defer ioc.Close()
func Close() {
	applicationContextInstance().Close()
}

func Exit(code int, format string, a ...any) {
	applicationContextInstance().exit(code, format, a...)
}

func AwaitTermination() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

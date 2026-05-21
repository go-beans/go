// Application lifecycle overview
//
//  0. Bean registration
//     Bean definitions are registered in the ApplicationContext.
//
// Refresh phase:
//
//  1. Bean instantiation
//     Non-lazy singleton beans are created.
//
//  2. Aware callbacks
//     BeanNameAware, EnvironmentAware, ApplicationContextAware, ...
//
//  3. Configuration and dependency injection
//     value tags, inject tags, configuration binding.
//
//  4. PostConstruct
//     Custom post-construct callback is invoked.
//
//  5. InitializingBean.AfterPropertiesSet()
//     Bean receives final initialization callback.
//
//  6. Lifecycle.Start()
//     Lifecycle beans are started by phase.
//
//  7. ContextRefreshedEvent
//     The context has been refreshed.
//
// Run phase:
//
//  8. ApplicationStartedEvent
//     Application has started, before runners.
//
//  9. ApplicationRunner.Run()
//     Application runners are executed by order.
//
//  10. a) ApplicationReadyEvent
//     Application is ready to serve.
//
//  10. b) ApplicationFailedEvent
//     Startup failed.
//
// # Application running
//
// Close phase:
//
//  11. ContextClosedEvent
//     Context shutdown has been requested.
//
//  12. Lifecycle.Stop()
//     Started lifecycle beans are stopped in reverse phase order.
//
//  13. PreDestroy
//     Custom pre-destroy callback is invoked.
//
//  14. DisposableBean.Destroy()
//     Bean receives final destroy callback.
package ioc

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"

	"github.com/go-errr/go/err"
	"github.com/go-external-config/go/lang"
	"github.com/go-external-config/go/util/reflects"
)

const InjectTag = "inject"
const Optional = "optional"

type Provider[T any] func() T

// Register bean
func Bean[T any]() *BeanDefinitionImpl[T] {
	return newBeanDefinition[T]()
}

// Resolve bean by type and (optionaly) name. Provide 'optional' as a second argument not to fail in case of a bean is not registered
func Resolve[T any](name ...string) Provider[T] {
	lang.Assert(len(name) <= 2, "Bean name and 'optional' expected")
	if len(name) == 2 {
		lang.Assert(name[1] == Optional, "Unsupported option '%s'", name[1])
		return newInjectQualifier[T]().Name(name[0]).Optional().resolveOrExit()
	} else if len(name) == 1 {
		return newInjectQualifier[T]().Name(name[0]).resolveOrExit()
	}
	return newInjectQualifier[T]().resolveOrExit()
}

// InjectBeans injects matching beans into fields tagged with `inject:""`.
// It is a manual equivalent of container-managed field injection.
func InjectBeans[T any](target *T) *T {
	injectBeansAny(target)
	return target
}

func injectBeansAny(target any) any {
	reflects.ForEachTaggedField(target, InjectTag, func(field reflects.Field) {
		name, optional := parseInjectTag(field)
		qualifier := InjectQualifier[any]{
			fieldName: field.Field.Name,
			t:         field.Type,
			name:      name,
			optional:  optional,
		}
		bean := qualifier.resolve()()
		if bean != nil {
			field.Value.Set(reflect.ValueOf(bean))
		}
	})
	return target
}

func parseInjectTag(field reflects.Field) (name string, optional bool) {
	parts := strings.Split(field.TagValue, ",")
	if len(parts) > 0 {
		name = strings.TrimSpace(parts[0])
	}
	for _, part := range parts[1:] {
		option := strings.TrimSpace(part)
		switch option {
		case "":
			continue
		case Optional:
			optional = true
		default:
			panic(err.NewIllegalArgumentException(fmt.Sprintf("Unsupported inject option '%s' used for %s %s", option, field.Field.Name, field.Field.Type)))
		}
	}
	return name, optional
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

// Graceful shutdown with non-zere exit code
func Exit(code int, format string, a ...any) {
	applicationContextInstance().exit(code, format, a...)
}

func AwaitTermination() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

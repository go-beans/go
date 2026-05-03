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

// Register bean
func Bean[T any]() *BeanDefinitionImpl[T] {
	return newBeanDefinition[T]()
}

// Inject bean by type and (optionaly) name
func Inject[T any](name ...string) func() T {
	lang.Assert(len(name) <= 1, "Optional bean name expected")
	if len(name) == 1 {
		return newInjectQualifier[T]().Name(name[0]).resolve()
	}
	return newInjectQualifier[T]().resolve()
}

func InjectBeans[T any](target *T) *T {
	InjectBeansAny(target)
	return target
}

func InjectBeansAny(target any) any {
	reflects.ForEachTaggedField(target, InjectTag, func(field reflects.Field) {
		name, optional := parseInjectTag(field)
		bean := applicationContextInstance().Bean(&InjectQualifier[any]{
			t:        field.Type,
			name:     name,
			optional: optional,
		})
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

func Exit(code int, format string, a ...any) {
	applicationContextInstance().exit(code, format, a...)
}

func AwaitTermination() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

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

// Resolve returns a lazy bean provider for the specified bean type and
// optionally bean name.
//
// The returned provider resolves the bean from the current ApplicationContext
// when invoked:
//
//	service := ioc.Resolve[MyService]()
//	service().Process()
//
// Resolve supports lazy on-demand bean creation. Explicit Refresh() is not
// required for ordinary bean usage because resolving a bean automatically
// creates the requested bean and all required dependencies.
//
// This makes Resolve convenient for:
//   - non-container-managed objects
//   - integration tests
//   - command-line utilities
//   - one-off service method invocations
//   - advanced/manual container usage
//
// For example:
//
//	ioc.Resolve[MyService]()().Process()
//
// may be sufficient without starting the full application lifecycle.
//
// Refresh() is only required when eager singleton initialization,
// bean post-processing, lifecycle startup, or application-wide container
// initialization semantics are needed.
//
// By default Resolve fails if the bean cannot be found. Pass optional=true
// to suppress failure and return the zero value instead.
//
// For container-managed beans prefer declarative dependency injection using
// inject tags instead of Resolve.
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

// InjectBeans injects matching beans into struct fields tagged with
// `inject:""`.
//
// InjectBeans is a manual equivalent of container-managed field injection
// and is primarily intended for tests, utilities, framework integration,
// or advanced/manual container usage where objects are created outside of
// the ApplicationContext.
//
// Typical usage:
//
//	type MyTest struct {
//		Service MyService `inject:""`
//	}
//
//	func TestSomething(t *testing.T) {
//		test := &MyTest{}
//		ioc.InjectBeans(test)
//
//		test.Service.Process()
//	}
//
// InjectBeans is not part of the normal application runtime flow.
// For container-managed application beans prefer ordinary dependency injection
// performed automatically by the ApplicationContext.
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

// Context returns the root context of the current ApplicationContext.
//
// The returned context is cancelled during ApplicationContext shutdown,
// after ContextClosedEvent is published and before lifecycle beans are
// stopped and bean destruction callbacks are executed.
//
// This allows background workers, listeners, asynchronous processors,
// schedulers, and long-running operations to react to graceful application
// shutdown using standard Go context cancellation semantics.
//
// Typical usage:
//
//	for {
//		select {
//		case <-reqContext.Done():
//			return
//
//		case <-ioc.Context().Done():
//			return
//
//		case msg := <-ch:
//			fmt.Println(msg)
//		}
//	}
//
// Context is primarily intended for cooperative shutdown of goroutines and
// infrastructure components managed by the application lifecycle.
func Context() context.Context {
	return applicationContextInstance().context
}

// Refresh initializes the current ApplicationContext.
//
// Refresh is the low-level container initialization operation, similar to
// Spring Framework's ApplicationContext refresh phase. It prepares the bean
// registry, creates required singleton beans, performs dependency injection,
// applies post-processing, and makes the context ready for use.
//
// Refresh does not start the full application runtime contract. In particular,
// it should not be treated as the application entry point. For normal
// applications prefer Run(), which performs refresh and then starts lifecycle
// beans and application runners.
//
// Typical use cases for Refresh are advanced/manual container setup,
// integration tests, or framework-level code that needs an initialized
// ApplicationContext without starting the whole application.
//
// In short:
//   - Refresh() creates and wires the container.
//   - Run() starts the application.
func Refresh() {
	applicationContextInstance().refresh()
}

// Run starts the application.
//
// Run is the high-level application entry point, similar to Spring Boot's
// SpringApplication.run(). It refreshes the ApplicationContext, starts
// lifecycle beans, invokes application runners, and publishes application
// lifecycle events.
//
// Prefer Run in main functions:
//
//	func main() {
//		defer ioc.Close()
//		ioc.Run()
//		ioc.AwaitTermination()
//	}
//
// Run should be used for real applications where the container is expected
// to manage startup lifecycle, long-running services, background workers,
// listeners, and application readiness.
//
// For tests or advanced scenarios where only bean creation and dependency
// injection are needed, use Refresh() instead.
func Run() {
	applicationContextInstance().run()
}

// Close gracefully shuts down the current ApplicationContext and releases
// all managed resources.
//
// The primary use case is deferred application shutdown in main:
//
//	defer ioc.Close()
//
// Close is intended to provide finally-block-like semantics for graceful
// container shutdown, ensuring lifecycle beans are stopped and cleanup
// callbacks are executed even if application startup or runtime processing
// fails.
//
// Close may also be used in integration tests to fully destroy the current
// ApplicationContext and recreate a clean container state between test runs.
//
// Multiple calls to Close are safe.
func Close() {
	applicationContextInstance().close()
}

// Exit gracefully closes the current ApplicationContext and terminates
// the process with the specified exit code.
//
// Exit never returns.
//
// Exit is intended only for exceptional application-wide termination cases,
// for example:
//   - fatal unrecoverable conditions requiring a specific process exit code
//   - command-line/batch applications returning operational status codes
//   - explicit user-triggered application termination (for example GUI "Exit")
//   - controlled process termination initiated by infrastructure components
//
// Exit should not be used for ordinary error handling, request processing,
// business flow control, or local goroutine termination.
//
// If another goroutine is already performing application shutdown, Exit
// terminates only the current goroutine and allows the original shutdown
// sequence to continue.
func Exit(code int, format string, a ...any) {
	applicationContextInstance().exit(code, format, a...)
}

// AwaitTermination blocks the current goroutine until the
// ApplicationContext begins shutdown.
//
// Run() is a non-blocking operation. The application lifecycle is managed by the
// ApplicationContext itself, while AwaitTermination is responsible only for
// preventing the main goroutine from exiting prematurely.
//
// Typical usage:
//
//	func main() {
//		defer ioc.Close()
//		ioc.Run()
//		ioc.AwaitTermination()
//	}
//
// AwaitTermination is primarily intended for main functions of long-running
// applications managed by go-beans lifecycle infrastructure.
//
// The method returns when application shutdown is initiated.
func AwaitTermination() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

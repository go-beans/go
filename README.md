# go-beans

[![Go Reference](https://pkg.go.dev/badge/github.com/go-beans/go.svg)](https://pkg.go.dev/github.com/go-beans/go)
[![Go Report Card](https://goreportcard.com/badge/github.com/go-beans/go)](https://goreportcard.com/report/github.com/go-beans/go)
[![Release](https://img.shields.io/github/v/release/go-beans/go)](https://github.com/go-beans/go/releases)

Lightweight, inversion of control implementation inspired by Spring, made for Go idioms

## Using the Container

The `ApplicationContext` is the service for an advanced factory capable of maintaining a registry of beans and their dependencies.  
For container-managed beans, use the `inject` field tag for dependencies:

```go
service type `inject:"optionalName"`
```

For non-container-managed objects:

```go
var service = ioc.Resolve[type]("optionalName")
```

## Bean Overview

An IoC container manages one or more beans. These beans are registered using the form that you supply to the container.

```go
ioc.Bean[type]().
    Scope("scope").
    Name("name").
    Profile("expression").
    Primary().
    Lazy().
    DependsOn("name").
    Phase(phase).
    Order(order).
    Factory(method).
    PostConstruct(method).
    PreDestroy(method).
    ApplicationListener(method).
    Register()
```

Within the container itself, these bean definitions are represented as `BeanDefinition` objects, which contain (among other information) the following metadata:

- A type of the bean being defined.
- Bean behavioral configuration elements, which state how the bean should behave in the container (scope, lifecycle callbacks, and so forth).

This metadata translates to a set of properties that make up each bean definition.

## Instantiating Beans

A bean definition is essentially a recipe for creating one or more objects. The container looks at the recipe for a named bean when asked and uses the configuration metadata encapsulated by that bean definition to create (or acquire) an actual object. `type` can be pointer to a struct `*MyService` or interface `MyInterface`. `*MyService` implements (can be assigned to) `MyInterface` if methods have a pointer receiver. You can provide `factory` as a reference to `func NewMyService() *MyService` or inline implementation.

Dedicate a file a.k.a. _context_ or _configuration_ for bean definitions inside package `init` method. One can reuse configured and ready-to-go services in different parts of application by for importing package where needed and injecting beans.

project/internal/package/context/context.go:

```go
func init() {
  ioc.Bean[*package.MyService]().Factory(package.NewMyService).Register()
  ioc.Bean[*cron.Cron]().Factory(func() *cron.Cron { return cron.New() }).
    PostConstruct((*cron.Cron).Start).
    PreDestroy(func(c *cron.Cron) { <-c.Stop().Done() }).Register()

  ioc.Bean[*pgxpool.Pool]().Factory(func() *pgxpool.Pool {
    url := env.Value[string]("postgres://${db.user}:${db.password}@${db.addr}/${db.name}")
    return optional.OfCommaErr(pgxpool.New(context.Background(), url)).OrElsePanic("Unable to create connection pool")
  }).PreDestroy((*pgxpool.Pool).Close).Register()

  ioc.Bean[*http.Client]().Factory(func() *http.Client {
    return env.ConfigurationProperties("component.httpClient", env.ConfigurationProperties("httpClient", &http.Client{
      Transport: env.ConfigurationProperties("component.httpClient.transport", env.ConfigurationProperties("httpClient.transport", &http.Transport{}))}))
  }).Register()

  ioc.Bean[*redis.Client]().Factory(func() *redis.Client {
    return redis.NewClient(env.ConfigurationProperties("component.redis", env.ConfigurationProperties("redis", &redis.Options{})))
  }).PreDestroy(func(c *redis.Client) { c.Close() }).Register()

  ioc.Bean[*concurrent.Executor[*redis.IntCmd]]().Name("publishExecutor").Factory(func() *concurrent.Executor[*redis.IntCmd] {
    return concurrent.NewExecutor[*redis.IntCmd](env.Value[int]("${component.publishParallelism}"))
  }).PreDestroy((*concurrent.Executor[*redis.IntCmd]).Close).Register()
}
```

## Dependencies

A typical enterprise application does not consist of a single object (or bean). Even the simplest application has a few objects that work together to present what the end-user sees as a coherent application. This next section explains how you go from defining a number of bean definitions that stand alone to a fully realized application where objects collaborate to achieve a goal.

### Dependency Injection

Dependency injection (DI) is a process whereby objects define their dependencies (that is, the other objects with which they work) only through arguments to a factory method, or properties that are set on the object instance after it is constructed or returned from a factory method. The container then injects those dependencies when it creates the bean. This process is fundamentally the inverse (hence the name, Inversion of Control) of the bean itself controlling the instantiation or location of its dependencies on its own by using direct construction of objects.

Code is cleaner with the DI principle, and decoupling is more effective when objects are provided with their dependencies. The object does not look up its dependencies and does not know the location or type of the dependencies. As a result, your services become easier to test, particularly when the dependencies are on interfaces, which allow for stub or mock implementations to be used in unit tests. Each service knows and cares only about its own dependencies when `Service A` uses `Service B` uses `Service C`.

project/internal/package/MyService.go:

```go
type MyService struct {
  httpClient  *http.Client  `inject:""`
  redisClient *redis.Client `inject:""`
  cron        *cron.Cron    `inject:""`
}

func NewMyService() *MyService {
  return &MyService{}
}

func (this *MyService) Run(args []string) {
  slog.Info(fmt.Sprintf("MyService: %v", this.httpClient.Get("http://example.com")))
}
```

project/cmd/package/main.go:

```go
package main

import _ "project/internal/package/context"

func main() {
  defer ioc.Close()
  ioc.Run()
  // ioc.AwaitTermination()
}
```

## Bean Scopes

When you create a bean definition, you create a recipe for creating actual instances of the class defined by that bean definition. The idea that a bean definition is a recipe is important, because it means that, as with a type, you can create many object instances from a single recipe.

You can control not only the various dependencies and configuration values that are to be plugged into an object that is created from a particular bean definition but also control the scope of the objects created from a particular bean definition. This approach is powerful and flexible, because you can choose the scope of the objects you create through configuration instead of having to bake in the scope of an object at the type level. Beans can be defined to be deployed in one of a number of scopes.

| Scope     | Description                                                                              |
| --------- | ---------------------------------------------------------------------------------------- |
| singleton | (Default) Scopes a single bean definition to a single object instance for IoC container. |
| prototype | Scopes a single bean definition to any number of object instances.                       |

### The Singleton Scope

Only one shared instance of a singleton bean is managed, and all requests for beans with a name that match that bean definition result in that one specific bean instance being returned by the container.

To put it another way, when you define a bean definition and it is scoped as a singleton, the IoC container creates exactly one instance of the object defined by that bean definition. This single instance is stored in a cache of such singleton beans, and all subsequent requests and references for that named bean return the cached object.

### The Prototype Scope

The non-singleton prototype scope of bean deployment results in the creation of a new bean instance every time a request for that specific bean is made. That is, the bean is injected into another bean or you request it through an `ioc.Resolve()` method call on the container. You may want to use the prototype scope for some stateful beans but note that `PreDestroy(method)` is called for `singleton` beans only for `ioc.Close()`.

## Lifecycle Callbacks

The container calls `PostConstruct(method)` after bean instantiation and lets a bean perform initialization work after the container has set all necessary properties on the bean. `PreDestroy(method)` lets a bean get a callback when the container that contains it is destroyed before graceful shutdown.

> Be aware that `PostConstruct` and initialization methods in general are executed within the container’s singleton creation lock. The bean instance is only considered as fully initialized and ready to be published to others after returning from the `PostConstruct` method. Such individual initialization methods are only meant for validating the configuration state and possibly preparing some data structures based on the given configuration but no further activity with external bean access.

## Application Lifecycle

The `ApplicationContext` manages the complete lifecycle of beans and application events.

### Startup Sequence

```text
0. Bean registration
   Bean definitions are registered in the ApplicationContext.

Refresh phase:

1. Bean instantiation
   Non-lazy singleton beans are created.

2. Aware callbacks
   BeanNameAware, EnvironmentAware, ApplicationContextAware, ...

3. Configuration and dependency injection
   value tags, inject tags, configuration binding.

4. PostConstruct
   Custom post-construct callback is invoked.

5. InitializingBean.AfterPropertiesSet()
   Bean receives final initialization callback.

6. Lifecycle.Start()
   Lifecycle beans are started by phase.

7. ContextRefreshedEvent
   The context has been refreshed.

Run phase:

8. ApplicationStartedEvent
   Application has started, before runners.

9. ApplicationRunner.Run()
   Application runners are executed by order.

10a. ApplicationReadyEvent
    Application is ready to serve.

10b. ApplicationFailedEvent
     Startup failed.
```

### Shutdown Sequence

```text
11. ContextClosedEvent
    Context shutdown has been requested.

12. Lifecycle.Stop()
    Started lifecycle beans are stopped in reverse phase order.

13. PreDestroy
    Custom pre-destroy callback is invoked.

14. DisposableBean.Destroy()
    Bean receives final destroy callback.
```

## Application Termination

go-beans supports both short-running batch applications and long-running applications.

If `ioc.AwaitTermination()` is not used, the application behaves like a batch job: `ioc.Run()` starts the context, executes application runners, publishes lifecycle events, and then the `main` function returns normally. Graceful shutdown still happens when `ioc.Close()` is deferred. `ioc.Close()` publishes `ContextClosedEvent`, stops `Lifecycle` beans, invokes destroy callbacks, and releases the global `ApplicationContext`.

```go
func main() {
	defer ioc.Close()
	ioc.Run()
}
```

For long-running applications, use `ioc.AwaitTermination()` to keep the main goroutine alive until the process receives a termination signal or the application is closed explicitly through `ioc.Exit`.

```go
func main() {
	defer ioc.Close()
	ioc.Run()
	ioc.AwaitTermination()
}
```

`ioc.Exit` gracefully closes the current ApplicationContext and terminates
the process with the specified exit code. It never returns.  
`ioc.Exit` is intended only for exceptional application-wide termination cases,
for example:

- fatal unrecoverable conditions requiring a specific process exit code
- command-line/batch applications returning operational status codes
- explicit user-triggered application termination (for example GUI "Exit")
- controlled process termination initiated by infrastructure components

## Application Events

go-beans provides support for application events and listeners.

### Built-in Application Events

| Event                     | Description                                                                                                                                                                                                                                                                                                                      |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ContextRefreshedEvent`   | Published when the `ApplicationContext` is initialized or refreshed by calling `Refresh()`. At this stage, all non-lazy singleton beans are instantiated, dependencies are injected, initialization callbacks are completed, and `Lifecycle` beans are started. The context is fully initialized and ready for use.              |
| `ApplicationStartedEvent` | Published when the application startup sequence begins after the context has been refreshed and before `ApplicationRunner` beans are executed.                                                                                                                                                                                   |
| `ApplicationReadyEvent`   | Published after all `ApplicationRunner` beans have completed. At this stage, the application is considered ready to serve requests and report readiness.                                                                                                                                                                         |
| `ApplicationFailedEvent`  | Published when application startup fails with an unrecovered error.                                                                                                                                                                                                                                                              |
| `ContextStartedEvent`     | Published when the `ApplicationContext` is explicitly started by calling `Start()`. Here, "started" means that all `Lifecycle` beans receive an explicit start signal. Typically, this event is used when restarting components after a previous `Stop()` call.                                                                  |
| `ContextStoppedEvent`     | Published when the `ApplicationContext` is explicitly stopped by calling `Stop()`. Here, "stopped" means that all started `Lifecycle` beans receive an explicit stop signal. A stopped context may later be restarted through `Start()`.                                                                                         |
| `ContextClosedEvent`      | Published when the `ApplicationContext` is being closed by calling `Close()`. At this stage, the application shutdown sequence begins, `Lifecycle` beans are about to be stopped, and singleton beans are about to be destroyed. Once closed, the context reaches the end of its lifecycle and cannot be refreshed or restarted. |

### Listening for Application Events

Register one method (or more) using `.ApplicationListener(...)`

```go
ioc.Bean[*UserService]().
	Factory(NewUserService).
	ApplicationListener((*UserService).OnApplicationReady).
	Register()
```

```go
// Method parameter type will be used to filer eligible events to pass to the consumer
func (this *UserService) OnApplicationReady(event *ioc.ApplicationReadyEvent) {
	slog.Info("application ready")
}
```

### Publishing Custom Application Events

Any reference or value type may be used as an event.

```go
type UserCreatedEvent struct {
	UserID int64
}
```

Publisher service needs `ApplicationContext` to publish events

```go
type Service2 struct {
	ctx *ioc.ApplicationContext
}

// Implements ApplicationContextAware
func (this *Service2) SetApplicationContext(ctx *ioc.ApplicationContext) {
	this.ctx = ctx
}

func (this *Service2) SomeMethod() {
    this.ctx.PublishEvent(&UserCreatedEvent{UserID: 123})
}
```

Consumer registration

```go
ioc.Bean[*app.Service1]().Factory(app.NewService1).
	ApplicationListener((*app.Service1).OnUserCreatedEvent).
	Register()
```

Consumer service will receive _**all**_ type of events which are assignable to `*UserCreatedEvent`

```go
func (this *Service1) OnUserCreated(event *UserCreatedEvent) {
	slog.Info(fmt.Sprintf("Service1: %v", event))
}
```

### Event Ordering

Application events are synchronous notifications. Application listeners are invoked according to bean ordering semantics:

- `Order(...)`
- `Ordered`

## Environment Abstraction

The `Environment` is an abstraction integrated in the container that models two key aspects of the application environment: `profiles` and `properties`.

A profile is a named, logical group of bean definitions to be registered with the container only if the given profile is active.

Properties play an important role in almost all applications and may originate from a variety of sources: properties files, system environment variables, command line parameters, and so on.

## Bean Definition Profiles

Bean definition profiles provide a mechanism in the core container that allows for registration of different beans in different environments. The word, “environment,” can mean different things to different users, and this feature can help with many use cases, including:

- Working against an in-memory datasource in development versus looking up some other datasource when in QA or production.
- Registering monitoring infrastructure only when deploying an application into a performance environment.
- Registering customized implementations of beans for customer A versus customer B deployments.

The profile string may contain a simple profile name (for example, `production`) or a profile expression. A profile expression allows for more complicated profile logic to be expressed (for example, `production & us-east`). The following operators are supported in profile expressions:

- `!`: A logical NOT of the profile
- `&`: A logical AND of the profiles
- `|`: A logical OR of the profiles

If a `Component` is marked with `Profile("p1,p2")`, that bean is not registered or processed unless profiles 'p1' or 'p2' have been activated. If a given profile is prefixed with the NOT operator (`!`), the annotated element is registered only if the profile is not active. For example, given `Profile("p1,!p2")`, registration will occur if profile 'p1' is active or if profile 'p2' is not active.

You can use a `profiles.active` `Environment` property to specify which profiles are active. You can specify the property in any of the ways described [here](https://github.com/go-external-config/go). For example, you could include it in your `application.properties`, as shown in the following example:

    profiles.active=dev,hsqldb

The `profiles.active` property follows the same ordering rules as other properties. The highest `PropertySource` wins. This means that you can specify active profiles in `application.properties` and then replace them by using the command line switch.

### Programmatically Setting Profiles

You can programmatically set active profiles by calling `env.SetActiveProfiles("...")` before your application runs. This can be useful for tests to mock `Bean`s or other scenarious.

## Transparent startup diagnostics

One common concern about dependency injection frameworks is that startup failures become difficult to debug because abstraction layers hide the original cause.

`go-beans` uses `go-errr/go` to work with errors and preserve call stacks with source file names and line numbers.  
Each wrapped error contributes its own stack frames, while common frames are omitted for readability.

The stack trace below should be read in two directions:

- wrapped errors (`Caused by`) are read top-to-bottom, with the root cause at the bottom;
- call stacks (`at ...`) are read bottom-to-top, with outer callers at the bottom.

Already started services are gracefully stopped.

```
D:\dev\playground>go run ./cmd/app
loading properties from config/application.yaml
loading properties from config/application-live.properties
2026/05/17 14:39:22 INFO ioc.ApplicationContext: starting with PID 11396
2026/05/17 14:39:22 DEBUG ioc.ApplicationContext: registering *app.Service1 [singleton lazy]
2026/05/17 14:39:22 DEBUG ioc.ApplicationContext: registering *app.Service2 [singleton Lifecycle]
2026/05/17 14:39:22 DEBUG ioc.ApplicationContext: registering *app.Service3 [singleton Lifecycle]
2026/05/17 14:39:22 DEBUG ioc.ApplicationContext: registering *app.Service4 [singleton service4]
2026/05/17 14:39:22 DEBUG ioc.ApplicationContext: registering *app.Service5 [singleton service5]
2026/05/17 14:39:22 DEBUG ioc.ApplicationContext: registering *app.ApplicationRunner1 [singleton lazy ApplicationRunner]
2026/05/17 14:39:22 DEBUG ioc.ApplicationContext: registering *app.ApplicationRunner2 [singleton ApplicationRunner]
2026/05/17 14:39:22 DEBUG ioc.ApplicationContext: registering *app.ApplicationRunner3 [singleton ApplicationRunner]
2026/05/17 14:39:22 INFO Service2.AfterPropertiesSet
2026/05/17 14:39:22 INFO Service3.AfterPropertiesSet
2026/05/17 14:39:22 INFO Service5.AfterPropertiesSet
2026/05/17 14:39:22 INFO Service4.AfterPropertiesSet
2026/05/17 14:39:22 INFO ApplicationRunner2.AfterPropertiesSet
2026/05/17 14:39:22 INFO ApplicationRunner3.AfterPropertiesSet
2026/05/17 14:39:22 INFO Service3.Start
2026/05/17 14:39:22 INFO Service2.Start
2026/05/17 14:39:22 INFO ioc.ApplicationContext: context refreshed in 1.1175ms
2026/05/17 14:39:26 ERROR Context run failed. *err.RuntimeException: Error creating bean *app.ApplicationRunner1 [singleton lazy ApplicationRunner]
        at github.com/go-beans/go/ioc.(*ApplicationContext).beanInstance.func1 (D:/dev/go-beans/ioc/ApplicationContext.go:140)
        at github.com/go-beans/go/ioc.(*ApplicationContext).beanInstance (D:/dev/go-beans/ioc/ApplicationContext.go:149)
        at github.com/go-beans/go/ioc.(*ApplicationContext).Bean (D:/dev/go-beans/ioc/ApplicationContext.go:133)
        at github.com/go-beans/go/ioc.(*InjectQualifier[...]).doResolve (D:/dev/go-beans/ioc/InjectQualifier.go:60)
        at github.com/go-beans/go/ioc.(*InjectQualifier[...]).resolve.func1.1 (D:/dev/go-beans/ioc/InjectQualifier.go:52)
        at sync.(*Once).doSlow (C:/Program Files/Go/src/sync/once.go:78)
        at sync.(*Once).Do (C:/Program Files/Go/src/sync/once.go:69)
        at github.com/go-beans/go/ioc.(*InjectQualifier[...]).resolve.func1 (D:/dev/go-beans/ioc/InjectQualifier.go:51)
        at github.com/go-beans/go/ioc.(*BeanDefinitionImpl[...]).instantiate.injectBeansAny.func2 (D:/dev/go-beans/ioc/ioc.go:84)
        at github.com/go-external-config/go/util/reflects.ForEachTaggedField (D:/dev/go-external-config/util/reflects/reflects.go:39)
        at github.com/go-beans/go/ioc.injectBeansAny (D:/dev/go-beans/ioc/ioc.go:76)
        at github.com/go-beans/go/ioc.(*BeanDefinitionImpl[...]).instantiate (D:/dev/go-beans/ioc/BeanDefinition.go:240)
        at github.com/go-beans/go/ioc.(*ApplicationContext).beanInstance.func2 (D:/dev/go-beans/ioc/ApplicationContext.go:152)
        at github.com/go-external-config/go/util/concurrent.Synchronized (D:/dev/go-external-config/util/concurrent/concurrent.go:11)
        at github.com/go-beans/go/ioc.(*ApplicationContext).beanInstance (D:/dev/go-beans/ioc/ApplicationContext.go:149)
        at github.com/go-beans/go/ioc.(*ApplicationContext).orderedBeanInstances.func1 (D:/dev/go-beans/ioc/ApplicationContext.go:268)
        at github.com/go-beans/go/ioc.(*ApplicationContext).foreachBeanDefinition (D:/dev/go-beans/ioc/ApplicationContext.go:427)
        at github.com/go-beans/go/ioc.(*ApplicationContext).orderedBeanInstances (D:/dev/go-beans/ioc/ApplicationContext.go:266)
        at github.com/go-beans/go/ioc.(*ApplicationContext).executeApplicationRunnerBeans (D:/dev/go-beans/ioc/ApplicationContext.go:320)
        at github.com/go-beans/go/ioc.(*ApplicationContext).Run (D:/dev/go-beans/ioc/ApplicationContext.go:315)
        at github.com/go-beans/go/ioc.Run (D:/dev/go-beans/ioc/ioc.go:135)
        at main.main (D:/dev/playground/cmd/app/main.go:33)
Caused by: *err.RuntimeException: Cannot inject dependency into field 'service1' of type *app.Service1
        at github.com/go-beans/go/ioc.(*ApplicationContext).Bean.func1 (D:/dev/go-beans/ioc/ApplicationContext.go:85)
        ... 20 common frames omitted
Caused by: *err.RuntimeException: Error creating bean *app.Service1 [singleton lazy]
        at github.com/go-beans/go/ioc.(*ApplicationContext).beanInstance.func1 (D:/dev/go-beans/ioc/ApplicationContext.go:140)
        at github.com/go-beans/go/ioc.(*ApplicationContext).beanInstance (D:/dev/go-beans/ioc/ApplicationContext.go:149)
        ... 20 common frames omitted
Caused by: *err.RuntimeException: Cannot bind configuration value '${vault.prod/db#password}' to field 'dbPass'
        at github.com/go-beans/go/ioc.(*BeanDefinitionImpl[...]).instantiate.BindPropertiesAny.func1.1 (D:/dev/go-external-config/env/env.go:58)
        at github.com/go-external-config/go/util/optional.(*Optional[...]).panicIfEmpty (D:/dev/go-external-config/util/optional/Optional.go:80)
        at github.com/go-external-config/go/util/optional.(*Optional[...]).OrElsePanic (D:/dev/go-external-config/util/optional/Optional.go:74)
        at github.com/go-external-config/vault/env.(*VaultPropertySource).getSecretValue (D:/dev/go-external-config-vault/env/VaultPropertySource.go:84)
        at github.com/go-external-config/vault/env.(*VaultPropertySource).resolveVaultProperty (D:/dev/go-external-config-vault/env/VaultPropertySource.go:80)
        at github.com/go-external-config/vault/env.(*VaultPropertySource).Property (D:/dev/go-external-config-vault/env/VaultPropertySource.go:60)
        at github.com/go-external-config/go/env.(*Environment).lookupRawProperty (D:/dev/go-external-config/env/Environment.go:81)
        at github.com/go-external-config/go/env.(*ExprProcessor).Resolve (D:/dev/go-external-config/env/ExprProcessor.go:64)
        at github.com/go-external-config/go/env.ExprProcessorOf.(*PatternProcessor).OverrideResolve.func1 (D:/dev/go-external-config/util/regex/PatternProcessor.go:68)
        at github.com/go-external-config/go/util/regex.(*PatternProcessor).ProcessRecursive (D:/dev/go-external-config/util/regex/PatternProcessor.go:42)
        at github.com/go-external-config/go/util/regex.(*PatternProcessor).Process (D:/dev/go-external-config/util/regex/PatternProcessor.go:24)
        at github.com/go-external-config/go/env.(*Environment).ResolveRequiredPlaceholders (D:/dev/go-external-config/env/Environment.go:89)
        at github.com/go-beans/go/ioc.(*BeanDefinitionImpl[...]).instantiate.BindPropertiesAny.func1 (D:/dev/go-external-config/env/env.go:60)
        at github.com/go-external-config/go/util/reflects.ForEachTaggedField (D:/dev/go-external-config/util/reflects/reflects.go:39)
        at github.com/go-external-config/go/env.BindPropertiesAny (D:/dev/go-external-config/env/env.go:56)
        at github.com/go-beans/go/ioc.(*BeanDefinitionImpl[...]).instantiate (D:/dev/go-beans/ioc/BeanDefinition.go:239)
        at github.com/go-beans/go/ioc.(*ApplicationContext).beanInstance.func2 (D:/dev/go-beans/ioc/ApplicationContext.go:152)
        at github.com/go-external-config/go/util/concurrent.Synchronized (D:/dev/go-external-config/util/concurrent/concurrent.go:11)
        ... 21 common frames omitted
Caused by: *err.RuntimeException: Unable to get prod/db
        at github.com/go-external-config/go/util/optional.(*Optional[...]).panicIfEmpty (D:/dev/go-external-config/util/optional/Optional.go:80)
        ... 37 common frames omitted
Caused by: *fmt.wrapError: error encountered while reading secret at secret/data/prod/db: Get "http://127.0.0.1:8200/v1/secret/data/prod/db": dial tcp 127.0.0.1:8200: connectex: No connection could be made because the target machine actively refused it.
Caused by: *url.Error: Get "http://127.0.0.1:8200/v1/secret/data/prod/db": dial tcp 127.0.0.1:8200: connectex: No connection could be made because the target machine actively refused it.
Caused by: *net.OpError: dial tcp 127.0.0.1:8200: connectex: No connection could be made because the target machine actively refused it.
Caused by: *os.SyscallError: connectex: No connection could be made because the target machine actively refused it.
Caused by: syscall.Errno: No connection could be made because the target machine actively refused it.
2026/05/17 14:39:26 INFO ioc.ApplicationContext: closing context with 8 running services
2026/05/17 14:39:26 INFO Service2.Stop
2026/05/17 14:39:26 INFO Service3.Stop
2026/05/17 14:39:26 INFO ioc.ApplicationContext: context closed in 17.8322ms, uptime 4.3038338s
exit status 1
```

## Credits

[The IoC Container](https://docs.spring.io/spring-framework/reference/core/beans.html)

## Installation

```bash
go get github.com/go-beans/go
```

## See also

[github.com/go-external-config/go](https://github.com/go-external-config/go)  
[github.com/go-errr/go](https://github.com/go-errr/go)

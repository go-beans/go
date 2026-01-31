# go-beans

[![Go Reference](https://pkg.go.dev/badge/github.com/go-beans/go.svg)](https://pkg.go.dev/github.com/go-beans/go)
[![Go Report Card](https://goreportcard.com/badge/github.com/go-beans/go)](https://goreportcard.com/report/github.com/go-beans/go)
[![Release](https://img.shields.io/github/v/release/go-beans/go)](https://github.com/go-beans/go/releases)

Lightweight, inversion of control implementation inspired by Spring, made for Go idioms

## Using the Container

The `ApplicationContext` is the service for an advanced factory capable of maintaining a registry of different beans and their dependencies. By using the method below, you can retrieve instances of your beans.

    service = ioc.Inject[type]("optionalName")

## Bean Overview

An IoC container manages one or more beans. These beans are registered using the form that you supply to the container.

    ioc.Bean[type]().Name("name").Scope("scope").Profile("expression").PostConstruct(method).PreDestroy(method).Factory(method).Register()

Within the container itself, these bean definitions are represented as `BeanDefinition` objects, which contain (among other information) the following metadata:

* A type of the bean being defined.
* Bean behavioral configuration elements, which state how the bean should behave in the container (scope, lifecycle callbacks, and so forth).

This metadata translates to a set of properties that make up each bean definition.

## Instantiating Beans

A bean definition is essentially a recipe for creating one or more objects. The container looks at the recipe for a named bean when asked and uses the configuration metadata encapsulated by that bean definition to create (or acquire) an actual object. `type` can be pointer to a struct `*MyService` or interface `MyInterface`. `*MyService` implements (can be assigned to) `MyInterface` if methods have a pointer receiver. You can provide `factory` as a reference to `func NewMyService() *MyService` or inline implementation like

	ioc.Bean[*cron.Cron]().Factory(func() *cron.Cron { return cron.New() }).PostConstruct((*cron.Cron).Start).PreDestroy(func(c *cron.Cron) { <-c.Stop().Done() }).Register()

	ioc.Bean[*pgxpool.Pool]().Factory(func() *pgxpool.Pool {
		url := env.Value[string]("postgres://${user}:${password}@${addr}/${database}")
		return optional.OfCommaErr(pgxpool.New(context.Background(), url)).OrElsePanic("Unable to create connection pool")
	}).PreDestroy((*pgxpool.Pool).Close).Register()

> Dedicate a file a.k.a. _context_ or _configuration_ for bean definitions inside package `init` method. One can reuse configured and ready-to-go services in different parts of application by for importing package where needed and injecting beans.

Actual instantiation is lazy and happens (once for default, singleton scope) when calling the provider method

	// somewhere inside factory method
	httpClient = ioc.Inject[*http.Client]()
	...
 	// somewhere inside method logic
	s.httpClient().Get("http://example.com") // lazy instantiation

main.go may look like the following:  

	package main

	import (...)

	var maintenanceJob = ioc.Inject[*MaintenanceJob]()

	func main() {
	    defer ioc.Close()

	    maintenanceJob().Run()

	    // ioc.AwaitTermination()
	}

## Dependencies

A typical enterprise application does not consist of a single object (or bean). Even the simplest application has a few objects that work together to present what the end-user sees as a coherent application. This next section explains how you go from defining a number of bean definitions that stand alone to a fully realized application where objects collaborate to achieve a goal.

### Dependency Injection

Dependency injection (DI) is a process whereby objects define their dependencies (that is, the other objects with which they work) only through arguments to a factory method, or properties that are set on the object instance after it is constructed or returned from a factory method. The container then injects those dependencies when it creates the bean. This process is fundamentally the inverse (hence the name, Inversion of Control) of the bean itself controlling the instantiation or location of its dependencies on its own by using direct construction of objects.

Code is cleaner with the DI principle, and decoupling is more effective when objects are provided with their dependencies. The object does not look up its dependencies and does not know the location or type of the dependencies. As a result, your service become easier to test, particularly when the dependencies are on interfaces, which allow for stub or mock implementations to be used in unit tests.

### Bean Scopes

When you create a bean definition, you create a recipe for creating actual instances of the class defined by that bean definition. The idea that a bean definition is a recipe is important, because it means that, as with a type, you can create many object instances from a single recipe.

You can control not only the various dependencies and configuration values that are to be plugged into an object that is created from a particular bean definition but also control the scope of the objects created from a particular bean definition. This approach is powerful and flexible, because you can choose the scope of the objects you create through configuration instead of having to bake in the scope of an object at the type level. Beans can be defined to be deployed in one of a number of scopes.

| Scope      | Description                                                                                  |
|------------|----------------------------------------------------------------------------------------------|
| singleton  | (Default) Scopes a single bean definition to a single object instance for IoC container.   |
| prototype  | Scopes a single bean definition to any number of object instances.                            |

### The Singleton Scope

Only one shared instance of a singleton bean is managed, and all requests for beans with a name that match that bean definition result in that one specific bean instance being returned by the container.

To put it another way, when you define a bean definition and it is scoped as a singleton, the IoC container creates exactly one instance of the object defined by that bean definition. This single instance is stored in a cache of such singleton beans, and all subsequent requests and references for that named bean return the cached object.

### The Prototype Scope

The non-singleton prototype scope of bean deployment results in the creation of a new bean instance every time a request for that specific bean is made. That is, the bean is injected into another bean or you request it through an `ioc.Inject()` method call on the container. You may want to use the prototype scope for some stateful beans but note that `PreDestroy(method)` is called for `singleton` beans only if `ioc.GracefulShutdown()` is configured.

### Lifecycle Callbacks

The container calls `PostConstruct(method)` after bean instantiation and lets a bean perform initialization work after the container has set all necessary properties on the bean. `PreDestroy(method)` lets a bean get a callback when the container that contains it is destroyed before graceful shutdown. 

> Be aware that @PostConstruct and initialization methods in general are executed within the container’s singleton creation lock. The bean instance is only considered as fully initialized and ready to be published to others after returning from the @PostConstruct method. Such individual initialization methods are only meant for validating the configuration state and possibly preparing some data structures based on the given configuration but no further activity with external bean access. Otherwise there is a risk for an initialization deadlock.

## Environment Abstraction

The `Environment` is an abstraction integrated in the container that models two key aspects of the application environment: `profiles` and `properties`.

A profile is a named, logical group of bean definitions to be registered with the container only if the given profile is active.

Properties play an important role in almost all applications and may originate from a variety of sources: properties files, system environment variables, command line parameters, and so on.

### Bean Definition Profiles

Bean definition profiles provide a mechanism in the core container that allows for registration of different beans in different environments. The word, “environment,” can mean different things to different users, and this feature can help with many use cases, including:

* Working against an in-memory datasource in development versus looking up some other datasource when in QA or production.
* Registering monitoring infrastructure only when deploying an application into a performance environment.
* Registering customized implementations of beans for customer A versus customer B deployments.

The profile string may contain a simple profile name (for example, `production`) or a profile expression. A profile expression allows for more complicated profile logic to be expressed (for example, `production & us-east`). The following operators are supported in profile expressions:

* `!`: A logical NOT of the profile
* `&`: A logical AND of the profiles
* `|`: A logical OR of the profiles

If a `Component` is marked with `Profile("p1,p2")`, that bean is not registered or processed unless profiles 'p1' or 'p2' have been activated. If a given profile is prefixed with the NOT operator (`!`), the annotated element is registered only if the profile is not active. For example, given `Profile("p1,!p2")`, registration will occur if profile 'p1' is active or if profile 'p2' is not active.

You can use a `profiles.active` `Environment` property to specify which profiles are active. You can specify the property in any of the ways described [here](https://github.com/go-external-config/go). For example, you could include it in your `application.properties`, as shown in the following example:

	profiles.active=dev,hsqldb

If no profile is active, a default profile is enabled.

The `profiles.active` property follows the same ordering rules as other properties. The highest `PropertySource` wins. This means that you can specify active profiles in `application.properties` and then replace them by using the command line switch.

### Programmatically Setting Profiles

You can programmatically set active profiles by calling `env.SetActiveProfiles("...")` before your application runs. This can be useful for tests to mock `Bean`s or other scenarious.

## Credits

[The IoC Container](https://docs.spring.io/spring-framework/reference/core/beans.html)

## Installation

```bash
go get github.com/go-beans/go
```

## See also
[github.com/go-external-config/go](https://github.com/go-external-config/go)

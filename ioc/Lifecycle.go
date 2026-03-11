package ioc

// see Phased
//
// 1. Bean instantiation (finished)
//
// 2. Dependency injection (finished)
//
// 3. Aware callbacks (finished)
//
// 4. PostConstruct (finished)
//
// 5. InitializingBean.AfterPropertiesSet() (finished)
//
// 6. Lfecycle.Start() (in-progress)
//
// 7. ContextRefreshedEvent
type Lifecycle interface {
	Start()
	Stop()
}

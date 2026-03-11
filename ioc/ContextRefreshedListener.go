package ioc

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
// 6. Lfecycle.Start() (finished)
//
// 7. ContextRefreshedEvent (in-progress)
type ContextRefreshedListener interface {
	OnContextRefreshed()
}

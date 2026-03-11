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
// 7. ContextRefreshedEvent (finished)
//
// 8. ApplicationRunner.Run() (finished)
//
// 9. ApplicationReadyEvent / ApplicationFailedEvent (finished)
//
// # APPLICATION RUNNING
//
// 10. ContextClosedEvent (finished)
//
// 11. Lfecycle.Stop() (finished)
//
// 12. PreDestroy (finished)
//
// 13. DisposableBean.Destroy() (in-progress)
type DisposableBean interface {
	Destroy()
}

package ioc

type ContextRefreshedEvent struct {
	Context *ApplicationContext
}

func NewContextRefreshedEvent(context *ApplicationContext) *ContextRefreshedEvent {
	return &ContextRefreshedEvent{Context: context}
}

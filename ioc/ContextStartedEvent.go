package ioc

type ContextStartedEvent struct{}

func NewContextStartedEvent() *ContextStartedEvent {
	return &ContextStartedEvent{}
}

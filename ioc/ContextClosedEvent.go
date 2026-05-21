package ioc

type ContextClosedEvent struct{}

func NewContextClosedEvent() *ContextClosedEvent {
	return &ContextClosedEvent{}
}

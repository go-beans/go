package ioc

type ContextStoppedEvent struct{}

func NewContextStoppedEvent() *ContextStoppedEvent {
	return &ContextStoppedEvent{}
}

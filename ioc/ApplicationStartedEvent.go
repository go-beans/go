package ioc

type ApplicationStartedEvent struct{}

func NewApplicationStartedEvent() *ApplicationStartedEvent {
	return &ApplicationStartedEvent{}
}

package ioc

type ApplicationFailedEvent struct {
	Error any
}

func NewApplicationFailedEvent(error any) *ApplicationFailedEvent {
	return &ApplicationFailedEvent{Error: error}
}

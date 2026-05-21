package ioc

import "time"

type ApplicationReadyEvent struct {
	StartupTime time.Duration
}

func NewApplicationReadyEvent(startupTime time.Duration) *ApplicationReadyEvent {
	return &ApplicationReadyEvent{StartupTime: startupTime}
}

package concurrent

import "sync"

func Atomic(mutex *sync.Mutex, operation func()) {
	mutex.Lock()
	defer mutex.Unlock()
	operation()
}

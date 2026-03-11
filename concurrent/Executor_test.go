package concurrent

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-external-config/go/lang"
)

func TestExecutorWithPanicHandling(t *testing.T) {
	t.Skip("for manual run")
	exec := NewExecutor[int](10)

	fmt.Printf("Submit %v\n", time.Now())
	futures := []Future[int]{}
	for i := 0; i < 50; i++ {
		futures = append(futures, exec.Submit(func() int {
			time.Sleep(time.Second)
			lang.AssertState(i%2 != 0, "simulated panic %d", i)
			return i * i
		}))
	}
	exec.Close()

	fmt.Printf("Consume %v\n", time.Now())
	for i, f := range futures {
		res, err := f.Result()
		// fmt.Printf("%d: %v, %v\n", i, res, err)
		if i%2 == 0 {
			lang.AssertState(err != nil, "expected panic for index %d, got result %d", i, res)
		} else {
			lang.AssertState(err == nil, "unexpected error for index %d: %v", i, err)
		}
	}
	fmt.Printf("Finish %v\n", time.Now())
}

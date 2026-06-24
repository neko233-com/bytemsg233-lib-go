package bytemsg233

import (
	"fmt"
	"sync"
	"testing"
)

type testPoolItem struct {
	Value int
}

func (x *testPoolItem) Reset() {
	x.Value = 0
}

func TestPoolConcurrentAcquireRelease(t *testing.T) {
	pool := NewPool(func() *testPoolItem {
		return &testPoolItem{}
	})

	const goroutineCount = 128
	const loopCount = 1000
	errCh := make(chan error, goroutineCount)

	var wg sync.WaitGroup
	wg.Add(goroutineCount)
	for workerIndex := 0; workerIndex < goroutineCount; workerIndex++ {
		go func(workerIndex int) {
			defer wg.Done()
			for loopIndex := 0; loopIndex < loopCount; loopIndex++ {
				item := pool.Acquire()
				if item == nil {
					errCh <- fmt.Errorf("Acquire returned nil")
					return
				}
				item.Value = workerIndex + loopIndex
				pool.Release(item)
			}
			errCh <- nil
		}(workerIndex)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

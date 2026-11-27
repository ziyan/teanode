package agent

import (
	"sync"
	"testing"
)

// The tool limits hold when the calls of a round arrive together.
func TestCallAllowanceHoldsUnderConcurrentCalls(t *testing.T) {
	run := &AskRun{settings: &AskSettings{CallLimits: map[string]int{"web_search": 12}}}
	var mutex sync.Mutex
	allowed := 0
	var group sync.WaitGroup
	for index := 0; index < 40; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if run.takeCallAllowance("web_search") {
				mutex.Lock()
				allowed++
				mutex.Unlock()
			}
		}()
	}
	group.Wait()
	if allowed != 12 {
		t.Fatalf("12 allowed, got %d", allowed)
	}
	if !run.takeCallAllowance("web_fetch") {
		t.Fatal("a tool with no limit has none")
	}
}

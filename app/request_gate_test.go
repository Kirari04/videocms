package app

import (
	"sync"
	"testing"
)

func TestRequestGateTryStartAdmitsOneConcurrentRequest(t *testing.T) {
	gate := NewRequestGate()
	const userID uint = 42
	const requests = 64

	for round := range 10 {
		start := make(chan struct{})
		admitted := make(chan bool, requests)
		var pending sync.WaitGroup
		for range requests {
			pending.Go(func() {
				<-start
				admitted <- gate.TryStart(userID)
			})
		}
		close(start)
		pending.Wait()
		close(admitted)

		count := 0
		for started := range admitted {
			if started {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("round %d: admitted %d requests, want 1", round, count)
		}
		if !gate.Blocked(userID) {
			t.Fatalf("round %d: admitted request did not block the user", round)
		}
		gate.End(userID)
		if gate.Blocked(userID) {
			t.Fatalf("round %d: user remains blocked after End", round)
		}
	}
}

func TestRequestGateTryStartAllowsDifferentUsers(t *testing.T) {
	gate := NewRequestGate()
	if !gate.TryStart(1) || !gate.TryStart(2) {
		t.Fatal("independent users should both be admitted")
	}
	gate.End(1)
	if !gate.TryStart(1) {
		t.Fatal("released user should be admitted again")
	}
	if gate.TryStart(2) {
		t.Fatal("releasing another user must not release the active user")
	}
	gate.End(1)
	gate.End(2)
}

func TestRequestGateTryStartRespectsGlobalBlock(t *testing.T) {
	gate := NewRequestGate()
	gate.block = true
	if gate.TryStart(1) {
		t.Fatal("globally blocked user should not be admitted")
	}
	if gate.active[1] {
		t.Fatal("rejected operation should not be marked active")
	}
	gate.Sync(false)
	if !gate.TryStart(1) {
		t.Fatal("user should be admitted after clearing the global block")
	}
	gate.End(1)
}

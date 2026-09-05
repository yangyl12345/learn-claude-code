package tasks

import (
	"sync"
	"testing"
)

func TestDependenciesAndAtomicClaim(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Create("a", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create("b", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependencies(b.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(b.ID, "worker"); err == nil {
		t.Fatal("blocked task was claimed")
	}
	if _, err := s.Claim(a.ID, "worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(a.ID, "worker"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	successes := 0
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Claim(b.ID, "different"); e == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("atomic claim successes=%d", successes)
	}
}

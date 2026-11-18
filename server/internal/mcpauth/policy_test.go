package mcpauth

import (
	"context"
	"sync"
	"testing"
)

func TestConcurrentServiceGrantsRemainIndependent(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		service := NewService(NewMemoryStore())
		var wg sync.WaitGroup
		for _, audience := range []string{Workflow, Collector} {
			wg.Add(1)
			go func(audience string) {
				defer wg.Done()
				if _, err := service.SetExternalServicePolicy(ctx, "user", audience, true, "admin"); err != nil {
					t.Error(err)
				}
			}(audience)
		}
		wg.Wait()
		p, err := service.ExternalPolicy(ctx, "user")
		if err != nil || !p.CanWrite || !p.CollectorCanWrite {
			t.Fatal("simultaneous grants lost an audience")
		}
	}
}

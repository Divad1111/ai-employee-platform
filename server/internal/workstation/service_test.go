package workstation_test

import (
	"context"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/workstation"
)

func TestWorkstationStatusFromPresence(t *testing.T) {
	ca, err := certca.NewDevAuthority()
	if err != nil {
		t.Fatal(err)
	}
	p := reliability.NewPresence(5, 15)
	svc := workstation.NewService(ca, p, nil)
	svc.EnsureRegistered(context.Background(), "WS-1", "node-1")
	p.Touch("WS-1", "v1", 1, 1, 10, 20, 30)
	v := svc.Get(context.Background(), "WS-1")
	if v.Status != reliability.StatusOnline {
		t.Fatal(v.Status)
	}
	if v.Name != "node-1" {
		t.Fatal(v.Name)
	}
	base := time.Now()
	p.SetClock(func() time.Time { return base.Add(20 * time.Second) })
	p.SweepOffline()
	v2 := svc.Get(context.Background(), "WS-1")
	if v2.Status != reliability.StatusOffline {
		t.Fatal(v2.Status)
	}
}

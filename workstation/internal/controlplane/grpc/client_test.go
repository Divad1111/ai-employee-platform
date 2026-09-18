package grpcclient_test

import (
	"context"
	"testing"
	"time"

	grpcclient "github.com/ai-employee-platform/workstation/internal/controlplane/grpc"
	"github.com/ai-employee-platform/workstation/internal/identity"
)

func TestDialRejectsIncompleteIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := grpcclient.Dial(ctx, "127.0.0.1:1", &identity.Bundle{})
	if err == nil {
		t.Fatal("身份不完整应失败")
	}
}

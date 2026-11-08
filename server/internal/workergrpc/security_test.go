package workergrpc_test

import (
	"context"
	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/workergrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
	"time"
)

func TestStreamAuthorizationAndRevocation(t *testing.T) {
	for _, mode := range []string{"foreign-workstation", "foreign-job", "revoke-idle", "revoke-send", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			ca, err := certca.NewDevAuthority()
			if err != nil {
				t.Fatal(err)
			}
			cert, err := workergrpc.LoadServerCertificate(ca, "localhost", "127.0.0.1")
			if err != nil {
				t.Fatal(err)
			}
			received := make(chan struct{}, 1)
			gs, lis, svc, err := workergrpc.ListenAndServeWith("127.0.0.1:0", ca, cert, workergrpc.Options{
				ValidateEvent: func(_ string, ev *aiev1.Event) error {
					if ev.JobId != "owned" {
						return status.Error(codes.PermissionDenied, "foreign job")
					}
					return nil
				},
				OnEvent: func(string, *aiev1.Event) { received <- struct{}{} },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer gs.Stop()
			cp, kp := mustClientCreds(t, ca, "WS-test")
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			conn := dial(t, ctx, lis.Addr().String(), cp, kp, ca.CAPEM())
			defer conn.Close()
			worker := aiev1.NewWorkerServiceClient(conn)
			stream, err := worker.Connect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := stream.Send(&aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Hello{Hello: &aiev1.WorkerHello{WorkstationId: "WS-test"}}}); err != nil {
				t.Fatal(err)
			}
			// A heartbeat confirms Hello is processed and the connection is registered.
			if err := stream.Send(&aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Heartbeat{Heartbeat: &aiev1.Heartbeat{}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := stream.Recv(); err != nil {
				t.Fatal(err)
			}
			expected := codes.PermissionDenied
			switch mode {
			case "foreign-workstation", "foreign-job":
				ev := &aiev1.Event{EventId: "test", WorkstationId: "WS-test", JobId: "owned", Type: aiev1.EventType_EVENT_TYPE_JOB_SUCCESS, Meta: &aiev1.EnvelopeMeta{Sequence: 1}}
				if mode == "foreign-workstation" {
					ev.WorkstationId = "WS-victim"
				} else {
					ev.JobId = "victim"
				}
				_ = stream.Send(&aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Event{Event: ev}})
			case "disconnect":
				svc.Disconnect("WS-test")
				expected = codes.Canceled
			default:
				rec := ca.FindByWorkstation("WS-test")
				if err := ca.Revoke(rec.Fingerprint); err != nil {
					t.Fatal(err)
				}
				if _, err := worker.Ping(ctx, &aiev1.PingRequest{}); status.Code(err) != codes.Unauthenticated {
					t.Fatalf("existing RPC accepted revoked certificate: %v", err)
				}
				if mode == "revoke-send" {
					_, err := svc.PushCommand("WS-test", aiev1.CommandType_COMMAND_TYPE_START_JOB, "E", "J", "{}")
					if err == nil {
						t.Fatal("sent command to revoked peer")
					}
				}
				expected = codes.Unauthenticated
			}
			if msg, err := stream.Recv(); status.Code(err) != expected {
				t.Fatalf("expected %v, got frame=%v err=%v", expected, msg, err)
			}
			select {
			case <-received:
				t.Fatal("unauthorized event dispatched")
			default:
			}
		})
	}
}

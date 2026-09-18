package workergrpc_test

import (
	"context"
	"io"
	"testing"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/workergrpc"
)

// TestConnectResumeHeartbeatEvent 覆盖 Resume 续传、Heartbeat、Event 幂等。
func TestConnectResumeHeartbeatEvent(t *testing.T) {
	ca, err := certca.NewDevAuthority()
	if err != nil {
		t.Fatal(err)
	}
	srvCert, err := workergrpc.LoadServerCertificate(ca, "localhost", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	cmds := reliability.NewCommandStore(0)
	events := reliability.NewEventStore(0)
	presence := reliability.NewPresence(5, 15)
	gs, lis, _, err := workergrpc.ListenAndServeWith("127.0.0.1:0", ca, srvCert, workergrpc.Options{
		CA: ca, Commands: cmds, Events: events, Presence: presence,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Stop()

	wsID := "WS-RESUME-1"
	certPEM, keyPEM := mustClientCreds(t, ca, wsID)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := dial(t, ctx, lis.Addr().String(), certPEM, keyPEM, ca.CAPEM())
	defer conn.Close()
	worker := aiev1.NewWorkerServiceClient(conn)

	c1, _, _ := cmds.Enqueue(wsID, aiev1.CommandType_COMMAND_TYPE_START_JOB, "E", "J1", `{}`)
	_, _, _ = cmds.Enqueue(wsID, aiev1.CommandType_COMMAND_TYPE_STOP_JOB, "E", "J1", `{}`)

	stream, err := worker.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Hello{Hello: &aiev1.WorkerHello{
		WorkstationId:            wsID,
		LastAckedCommandSequence: 0,
		AgentVersion:             "test",
	}}}); err != nil {
		t.Fatal(err)
	}

	var lastSeq uint64
	for i := 0; i < 2; i++ {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		cmd := msg.GetCommand()
		if cmd == nil {
			t.Fatalf("期望 Command, got %T", msg.Body)
		}
		lastSeq = cmd.GetMeta().GetSequence()
		if err := stream.Send(&aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_CommandAck{CommandAck: &aiev1.CommandAck{
			CommandId: cmd.CommandId, Sequence: lastSeq, Accepted: true,
		}}}); err != nil {
			t.Fatal(err)
		}
		_ = cmds.Ack(wsID, cmd.CommandId, lastSeq)
	}
	if lastSeq < 1 || c1.CommandId == "" {
		t.Fatal("未完成 Resume 续传")
	}
	if presence.StatusOf(wsID) != reliability.StatusOnline {
		t.Fatal("连接后应 ONLINE")
	}

	if err := stream.Send(&aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Heartbeat{Heartbeat: &aiev1.Heartbeat{
		Meta:          &aiev1.EnvelopeMeta{MessageId: "hb1", TimestampUnixMs: time.Now().UnixMilli()},
		WorkstationId: wsID, Version: "t",
	}}}); err != nil {
		t.Fatal(err)
	}
	hbAck, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if hbAck.GetHeartbeatAck() == nil {
		t.Fatal("期望 HeartbeatAck")
	}

	ev := &aiev1.Event{
		EventId: "ev-1", WorkstationId: wsID,
		Meta: &aiev1.EnvelopeMeta{MessageId: "me1", Sequence: 1, TimestampUnixMs: time.Now().UnixMilli()},
		Type: aiev1.EventType_EVENT_TYPE_JOB_SUCCESS,
	}
	for i := 0; i < 2; i++ {
		if err := stream.Send(&aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Event{Event: ev}}); err != nil {
			t.Fatal(err)
		}
		ea, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if !ea.GetEventAck().GetAccepted() {
			t.Fatalf("event ack: %+v", ea.GetEventAck())
		}
	}

	_ = stream.CloseSend()
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}

	// Workstation 重启：携带 last_acked，不应再收到已确认命令
	stream2, err := worker.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream2.Send(&aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Hello{Hello: &aiev1.WorkerHello{
		WorkstationId:            wsID,
		LastAckedCommandSequence: lastSeq,
		AgentVersion:             "test",
	}}}); err != nil {
		t.Fatal(err)
	}
	_ = stream2.CloseSend()
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case <-deadline:
			return
		default:
			msg, err := stream2.Recv()
			if err == io.EOF || err != nil {
				return
			}
			if msg.GetCommand() != nil {
				t.Fatalf("Resume 后不应再下发已 ACK 命令: %s", msg.GetCommand().CommandId)
			}
		}
	}
}

// TestDuplicateCommandIdempotentOnServer Resume 重投同一未确认命令保持幂等队列。
func TestServerRestartKeepsCommandStore(t *testing.T) {
	ca, err := certca.NewDevAuthority()
	if err != nil {
		t.Fatal(err)
	}
	srvCert, err := workergrpc.LoadServerCertificate(ca, "localhost", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	// 共享 CommandStore 模拟「进程重启但状态恢复」
	cmds := reliability.NewCommandStore(0)
	wsID := "WS-RESTART"
	cmd, _, _ := cmds.Enqueue(wsID, aiev1.CommandType_COMMAND_TYPE_SYNC_WORKSPACE, "", "", `{}`)

	gs1, lis1, _, err := workergrpc.ListenAndServeWith("127.0.0.1:0", ca, srvCert, workergrpc.Options{
		CA: ca, Commands: cmds,
	})
	if err != nil {
		t.Fatal(err)
	}
	gs1.Stop()
	_ = lis1

	gs2, lis2, _, err := workergrpc.ListenAndServeWith("127.0.0.1:0", ca, srvCert, workergrpc.Options{
		CA: ca, Commands: cmds,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gs2.Stop()

	certPEM, keyPEM := mustClientCreds(t, ca, wsID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dial(t, ctx, lis2.Addr().String(), certPEM, keyPEM, ca.CAPEM())
	defer conn.Close()
	stream, err := aiev1.NewWorkerServiceClient(conn).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Hello{Hello: &aiev1.WorkerHello{
		WorkstationId: wsID, LastAckedCommandSequence: 0,
	}}})
	msg, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if msg.GetCommand().GetCommandId() != cmd.CommandId {
		t.Fatalf("重启后续传失败")
	}
}

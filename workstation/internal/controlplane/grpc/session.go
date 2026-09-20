package grpcclient

import (
	"context"
	"io"
	"sync"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/config"
	"github.com/ai-employee-platform/workstation/internal/controlplane/ack"
	"github.com/ai-employee-platform/workstation/internal/controlplane/heartbeat"
	"github.com/ai-employee-platform/workstation/internal/controlplane/outbox"
	"github.com/ai-employee-platform/workstation/internal/controlplane/reconnect"
	"github.com/ai-employee-platform/workstation/internal/controlplane/resume"
	"github.com/ai-employee-platform/workstation/internal/controlplane/sequence"
)

// Session 管理 Connect 双向流：Resume、Heartbeat、ACK、Outbox。
type Session struct {
	Client    *Client
	Handler   *ack.Handler
	Outbox    *outbox.Dispatcher
	Backoff   *reconnect.Backoff
	Validator *sequence.Validator

	HeartbeatInterval time.Duration
	AgentVersion      string
	OnCommand         func(ctx context.Context, cmd *aiev1.Command) error
	Stats             heartbeat.StatsFunc

	mu     sync.Mutex
	stream aiev1.WorkerService_ConnectClient
	sendMu sync.Mutex
}

// NewSession 组装会话（调用方注入 Journal）。
func NewSession(cli *Client, journal ack.Journal) *Session {
	v := sequence.NewValidator(0)
	if seq, err := journal.LastAckedSequence(); err == nil {
		v.SetLastSequence(seq)
	}
	return &Session{
		Client:            cli,
		Handler:           &ack.Handler{Journal: journal, Validator: v},
		Validator:         v,
		Backoff:           reconnect.NewBackoff(60 * time.Second),
		HeartbeatInterval: 5 * time.Second,
		AgentVersion:      config.Version,
	}
}

// attachOutbox 将 Outbox 发送绑定到当前流。
func (s *Session) attachOutbox() {
	if s.Outbox == nil {
		return
	}
	s.Outbox.Send = func(ctx context.Context, ev *aiev1.Event) error {
		return s.send(ctx, &aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Event{Event: ev}})
	}
}

func (s *Session) send(_ context.Context, msg *aiev1.WorkerToServer) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.mu.Lock()
	stream := s.stream
	s.mu.Unlock()
	if stream == nil {
		return io.ErrClosedPipe
	}
	return stream.Send(msg)
}

// Run 维持连接：断线 → 指数退避重连 → Resume。ctx 取消后退出。
func (s *Session) Run(ctx context.Context) error {
	s.attachOutbox()
	var outboxCancel context.CancelFunc
	if s.Outbox != nil {
		var octx context.Context
		octx, outboxCancel = context.WithCancel(ctx)
		go func() { _ = s.Outbox.Run(octx) }()
		defer outboxCancel()
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := s.runOnce(ctx)
		s.Backoff.MarkDisconnected()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wait := s.Backoff.BeginReconnect()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		_ = err
	}
}

func (s *Session) runOnce(ctx context.Context) error {
	stream, err := s.Client.worker.Connect(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.stream = stream
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.stream = nil
		s.mu.Unlock()
		_ = stream.CloseSend()
	}()

	lastAck, _ := s.Handler.Journal.LastAckedSequence()
	hello := resume.BuildHello(s.Client.wsID, s.AgentVersion, lastAck)
	if err := stream.Send(resume.HelloFrame(hello)); err != nil {
		return err
	}
	s.Backoff.MarkConnected()

	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	hb := &heartbeat.Loop{
		WorkstationID: s.Client.wsID,
		Version:       s.AgentVersion,
		Interval:      s.HeartbeatInterval,
		Stats:         s.Stats,
		Send: func(c context.Context, h *aiev1.Heartbeat) error {
			return s.send(c, &aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Heartbeat{Heartbeat: h}})
		},
	}
	go func() { _ = hb.Run(hbCtx) }()

	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		if cmd := msg.GetCommand(); cmd != nil {
			ackMsg, should, err := s.Handler.Process(cmd)
			if err != nil || !should {
				continue // COMMIT 失败不 ACK
			}
			_ = s.send(ctx, &aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_CommandAck{CommandAck: ackMsg}})
			if s.OnCommand != nil && ackMsg.Accepted {
				go func(c *aiev1.Command) {
					_ = s.OnCommand(ctx, c)
				}(cmd)
			}
		}
		// HeartbeatAck / EventAck 可忽略或记日志
	}
}

// EnqueueEvent 经 Outbox 上报（断网不丢）。
func (s *Session) EnqueueEvent(ev *aiev1.Event) error {
	if s.Outbox == nil {
		return s.send(context.Background(), &aiev1.WorkerToServer{Body: &aiev1.WorkerToServer_Event{Event: ev}})
	}
	return s.Outbox.Enqueue(ev)
}

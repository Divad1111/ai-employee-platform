// Package workergrpc 实现 Workstation 出站连接的 gRPC 服务端（mTLS）。
// 设计依据：设计文档 §20、§64、§21–§24、§67。
package workergrpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/reliability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Options 组装可靠通信依赖。
type Options struct {
	CA       *certca.Authority
	Commands *reliability.CommandStore
	Events   *reliability.EventStore
	Presence *reliability.Presence
	OnEvent  func(wsID string, ev *aiev1.Event)
}

// Server 实现 aiev1.WorkerServiceServer。
type Server struct {
	aiev1.UnimplementedWorkerServiceServer
	CA       *certca.Authority
	Commands *reliability.CommandStore
	Events   *reliability.EventStore
	Presence *reliability.Presence
	OnEvent  func(wsID string, ev *aiev1.Event)

	mu      sync.Mutex
	streams map[string]*connSession // workstation_id → 当前连接
}

type connSession struct {
	wsID   string
	stream aiev1.WorkerService_ConnectServer
	cancel context.CancelFunc
}

// NewServer 创建带可靠通信能力的 gRPC 服务。
func NewServer(opt Options) *Server {
	if opt.Commands == nil {
		opt.Commands = reliability.NewCommandStore(0)
	}
	if opt.Events == nil {
		opt.Events = reliability.NewEventStore(0)
	}
	if opt.Presence == nil {
		opt.Presence = reliability.NewPresence(5, 15)
	}
	return &Server{
		CA:       opt.CA,
		Commands: opt.Commands,
		Events:   opt.Events,
		Presence: opt.Presence,
		OnEvent:  opt.OnEvent,
		streams:  make(map[string]*connSession),
	}
}

// Ping 连通性与 mTLS 探测。
func (s *Server) Ping(ctx context.Context, req *aiev1.PingRequest) (*aiev1.PingResponse, error) {
	wsID, err := workstationFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetWorkstationId() != "" && req.GetWorkstationId() != wsID {
		return nil, status.Error(codes.PermissionDenied, "workstation_id 与证书不符")
	}
	return &aiev1.PingResponse{
		Nonce:                  req.GetNonce(),
		ServerUnixMs:           time.Now().UnixMilli(),
		CurrentCommandSequence: s.Commands.CurrentSequence(wsID),
	}, nil
}

// Connect 双向流：首帧 Hello（Resume）→ 续传未确认命令；上行 Event/Heartbeat/Ack。
func (s *Server) Connect(stream aiev1.WorkerService_ConnectServer) error {
	wsID, err := workstationFromCtx(stream.Context())
	if err != nil {
		return err
	}

	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument, "首帧必须是 WorkerHello")
	}
	if hello.GetWorkstationId() != "" && hello.GetWorkstationId() != wsID {
		return status.Error(codes.PermissionDenied, "hello.workstation_id 与证书不符")
	}

	// Workstation 重连后事件序号从 1 重计，必须重置游标，否则 JOB_* 全部被乱序拒绝。
	s.Events.ResetWorkstation(wsID)

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	sess := &connSession{wsID: wsID, stream: stream, cancel: cancel}
	s.mu.Lock()
	if old, ok := s.streams[wsID]; ok {
		old.cancel()
	}
	s.streams[wsID] = sess
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.streams[wsID] == sess {
			delete(s.streams, wsID)
		}
		s.mu.Unlock()
	}()

	s.Presence.MarkOnline(wsID)

	// Resume：从 last_acked+1 续传未确认命令
	for _, cmd := range s.Commands.UnackedAfter(wsID, hello.GetLastAckedCommandSequence()) {
		if err := stream.Send(&aiev1.ServerToWorker{Body: &aiev1.ServerToWorker_Command{Command: cmd}}); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return nil
		}
		if err := s.handleUpstream(wsID, stream, msg); err != nil {
			// 单帧错误不拆连接；带 error 的 ACK 已回写
			_ = err
		}
	}
}

func (s *Server) handleUpstream(wsID string, stream aiev1.WorkerService_ConnectServer, msg *aiev1.WorkerToServer) error {
	switch body := msg.GetBody().(type) {
	case *aiev1.WorkerToServer_Heartbeat:
		hb := body.Heartbeat
		cpu, mem, disk := 0.0, 0.0, 0.0
		if r := hb.GetResources(); r != nil {
			cpu, mem, disk = r.CpuPercent, r.MemoryPercent, r.DiskPercent
		}
		s.Presence.Touch(wsID, hb.GetVersion(), hb.GetEmployees(), hb.GetSessions(), cpu, mem, disk)
		return stream.Send(&aiev1.ServerToWorker{
			Body: &aiev1.ServerToWorker_HeartbeatAck{HeartbeatAck: &aiev1.HeartbeatAck{
				MessageId:       hb.GetMeta().GetMessageId(),
				ServerUnixMs:    time.Now().UnixMilli(),
				NextIntervalSec: uint32(s.Presence.HeartbeatInterval().Seconds()),
			}},
		})
	case *aiev1.WorkerToServer_Event:
		ev := body.Event
		if ev.GetWorkstationId() == "" {
			ev.WorkstationId = wsID
		}
		res := s.Events.Accept(ev)
		if !res.Accepted && res.Error != "" {
			fmt.Printf("event rejected ws=%s id=%s seq=%d: %s\n", wsID, ev.GetEventId(), ev.GetMeta().GetSequence(), res.Error)
		}
		if s.OnEvent != nil && res.Accepted && !res.Duplicate {
			go s.OnEvent(wsID, ev)
		}
		return stream.Send(&aiev1.ServerToWorker{
			Body: &aiev1.ServerToWorker_EventAck{EventAck: &aiev1.EventAck{
				EventId:      ev.GetEventId(),
				Sequence:     ev.GetMeta().GetSequence(),
				Accepted:     res.Accepted,
				ErrorMessage: res.Error,
			}},
		})
	case *aiev1.WorkerToServer_CommandAck:
		ack := body.CommandAck
		err := s.Commands.Ack(wsID, ack.GetCommandId(), ack.GetSequence())
		if err != nil {
			return err
		}
		return nil
	case *aiev1.WorkerToServer_Hello:
		return status.Error(codes.FailedPrecondition, "重复 Hello")
	default:
		return status.Error(codes.InvalidArgument, "未知上行帧")
	}
}

// PushCommand 入队并尝试经当前连接下发。
func (s *Server) PushCommand(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string) (*aiev1.Command, error) {
	return s.PushCommandFull(wsID, typ, employeeID, jobID, payloadJSON, nil)
}

// PushCommandFull 入队并下发，可附加结构化载荷。
func (s *Server) PushCommandFull(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string, apply func(*aiev1.Command)) (*aiev1.Command, error) {
	cmd, _, err := s.Commands.EnqueueFull(wsID, typ, employeeID, jobID, payloadJSON, apply)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	sess := s.streams[wsID]
	s.mu.Unlock()
	if sess == nil {
		return cmd, nil // 入队，等 Resume
	}
	if err := sess.stream.Send(&aiev1.ServerToWorker{Body: &aiev1.ServerToWorker_Command{Command: cmd}}); err != nil {
		return cmd, fmt.Errorf("下发失败（已入队）: %w", err)
	}
	return cmd, nil
}

// StartPresenceSweeper 后台扫描 Offline。
func (s *Server) StartPresenceSweeper(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Second
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.Presence.SweepOffline()
			}
		}
	}()
}

func workstationFromCtx(ctx context.Context) (string, error) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return "", status.Error(codes.Unauthenticated, "缺少对等身份")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return "", status.Error(codes.Unauthenticated, "缺少客户端证书")
	}
	return tlsInfo.State.PeerCertificates[0].Subject.CommonName, nil
}

// ListenAndServe 启动 mTLS gRPC（TLS 1.3）。
func ListenAndServe(addr string, ca *certca.Authority, serverCert tls.Certificate) (*grpc.Server, net.Listener, *Server, error) {
	return ListenAndServeWith(addr, ca, serverCert, Options{CA: ca})
}

// ListenAndServeWith 使用自定义 Options 启动。
func ListenAndServeWith(addr string, ca *certca.Authority, serverCert tls.Certificate, opt Options) (*grpc.Server, net.Listener, *Server, error) {
	opt.CA = ca
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(ca.CAPEM()); !ok {
		return nil, nil, nil, fmt.Errorf("加载 CA 失败")
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("无客户端证书")
			}
			_, _, err := ca.VerifyClientRaw(rawCerts[0])
			return err
		},
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, nil, err
	}
	svc := NewServer(opt)
	gs := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsCfg)))
	aiev1.RegisterWorkerServiceServer(gs, svc)
	go func() { _ = gs.Serve(lis) }()
	return gs, lis, svc, nil
}

// LoadServerCertificate 用 CA 签发并加载服务端 TLS 证书。
func LoadServerCertificate(ca *certca.Authority, hosts ...string) (tls.Certificate, error) {
	if len(hosts) == 0 {
		hosts = []string{"localhost", "127.0.0.1"}
	}
	certPEM, keyPEM, err := ca.SignServerCertificate("control-plane", hosts, 365)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

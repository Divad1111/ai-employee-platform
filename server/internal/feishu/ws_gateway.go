// Package feishu: 飞书官方 WebSocket 长连接网关接入。
// 依据飞书官方 Go SDK (larkws) 实现主动出站长连接，彻底摆脱公网 IP 与内网穿透依赖。
package feishu

import (
	"context"
	"fmt"
	"sync"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// WSStatus 长连接状态描述
type WSStatus struct {
	State        string    `json:"state"` // DISCONNECTED / CONNECTING / CONNECTED / ERROR
	ErrorMessage string    `json:"error_message,omitempty"`
	ConnectedAt  time.Time `json:"connected_at,omitempty"`
}

// WSGateway 飞书 WebSocket 长连接网关管理器
type WSGateway struct {
	mu          sync.Mutex
	client      *larkws.Client
	cancel      context.CancelFunc
	state       string
	errMessage  string
	connectedAt time.Time
}

// NewWSGateway 创建长连接网关
func NewWSGateway() *WSGateway {
	return &WSGateway{
		state: "DISCONNECTED",
	}
}

// Status 获取当前长连接状态
func (g *WSGateway) Status() WSStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	return WSStatus{
		State:        g.state,
		ErrorMessage: g.errMessage,
		ConnectedAt:  g.connectedAt,
	}
}

// Stop 停止当前长连接
func (g *WSGateway) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopLocked()
}

// Restart 尝试重启长连接网关（兼容调用）
func (g *WSGateway) Restart(_ context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return nil
}

func (g *WSGateway) stopLocked() {
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
	if g.client != nil {
		g.client.Close()
		g.client = nil
	}
	g.state = "DISCONNECTED"
	g.errMessage = ""
}

// Start 启动或重启长连接网关
func (g *WSGateway) Start(appID, appSecret string, handler *dispatcher.EventDispatcher) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.stopLocked()

	if appID == "" || appSecret == "" || handler == nil {
		g.state = "DISCONNECTED"
		return nil
	}

	runCtx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.state = "CONNECTING"
	g.errMessage = ""

	cli := larkws.NewClient(
		appID,
		appSecret,
		larkws.WithEventHandler(handler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
		larkws.WithAutoReconnect(true),
		larkws.WithOnReady(func() {
			g.mu.Lock()
			g.state = "CONNECTED"
			g.errMessage = ""
			g.connectedAt = time.Now().UTC()
			g.mu.Unlock()
		}),
		larkws.WithOnError(func(err error) {
			g.mu.Lock()
			g.state = "ERROR"
			g.errMessage = err.Error()
			g.mu.Unlock()
		}),
		larkws.WithOnReconnecting(func() {
			g.mu.Lock()
			g.state = "CONNECTING"
			g.mu.Unlock()
		}),
		larkws.WithOnDisconnected(func() {
			g.mu.Lock()
			g.state = "DISCONNECTED"
			g.mu.Unlock()
		}),
	)

	g.client = cli

	go func() {
		if err := cli.Start(runCtx); err != nil && runCtx.Err() == nil {
			g.mu.Lock()
			g.state = "ERROR"
			g.errMessage = fmt.Sprintf("长连接退出: %v", err)
			g.mu.Unlock()
		}
	}()

	return nil
}

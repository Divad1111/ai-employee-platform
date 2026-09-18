// Package recovery 处理崩溃/断连/进程消失。
// PID 不存在 → Session/Job UNKNOWN，不标 SUCCESS。
// 设计依据：设计文档 §57、§53、§91。
package recovery

import (
	"context"

	"github.com/ai-employee-platform/workstation/internal/runtime"
	"github.com/ai-employee-platform/workstation/internal/runtime/process"
)

// Manager 恢复管理。
type Manager struct {
	Runtime *runtime.Managers
	Proc    *process.Manager
}

// CheckProcessCrash 检查会话对应进程是否消失。
func (m *Manager) CheckProcessCrash(ctx context.Context, sessionID, jobID string) (crashed bool, err error) {
	if m.Proc == nil {
		return false, nil
	}
	info, err := m.Proc.Inspect(sessionID)
	if err == process.ErrNotFound {
		m.Runtime.MarkUnknown(sessionID, jobID)
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if info != nil && !info.Running {
		m.Runtime.MarkUnknown(sessionID, jobID)
		return true, nil
	}
	_ = ctx
	return false, nil
}

// OnACPDisconnect ACP 断开。
func (m *Manager) OnACPDisconnect(sessionID, jobID string) {
	m.Runtime.MarkUnknown(sessionID, jobID)
}

// OnNetworkUnavailable 网络不可用（最小处理：标记未知，Outbox 自行积压）。
func (m *Manager) OnNetworkUnavailable(sessionID, jobID string) {
	m.Runtime.MarkUnknown(sessionID, jobID)
}

// BootRecover 启动时恢复：未知态不标 SUCCESS。
func (m *Manager) BootRecover() {
	m.Runtime.RecoverRunning()
}

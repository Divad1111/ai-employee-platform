/**
 * Audit Logs 操作审计日志 / Settings 系统环境与架构参数配置。
 */
import { useEffect, useState } from 'react'
import { apiGet, apiPost } from '../api/client'
import { IconFileText, IconRefresh } from '../components/Icons'

type AuditItem = {
  id: number
  actor_id: string
  action: string
  result: string
  created_at: string
}

export function AuditPage() {
  const [items, setItems] = useState<AuditItem[]>([])
  const [action, setAction] = useState('')
  const [loading, setLoading] = useState(false)

  const load = () => {
    setLoading(true)
    const q = action ? `?action=${encodeURIComponent(action)}&limit=50` : '?limit=50'
    apiGet<{ items: AuditItem[] }>(`/audit${q}`)
      .then((d) => setItems(d.items ?? []))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
  }, [action])

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>系统安全与操作审计日志 (Audit Logs)</h1>
          <p>只追加的高级安全审计流 · 全量记录权限判定、敏感机密调用与审批操作记录</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => load()} disabled={loading}>
          <IconRefresh size={15} />
          <span>刷新审计日志</span>
        </button>
      </header>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>操作检索与过滤</h2>
            <p>可按操作动作 (例: secret.access、approval.decide) 筛选最近 50 条记录</p>
          </div>
        </div>
        <div className="inline-form">
          <input
            placeholder="输入动作 Action 过滤 (例如: secret.access)"
            value={action}
            onChange={(e) => setAction(e.target.value)}
            style={{ width: '320px' }}
          />
          {action ? (
            <button type="button" className="btn-ghost" onClick={() => setAction('')}>
              清除筛选
            </button>
          ) : null}
        </div>
      </div>

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>审计轨迹流水</h2>
            <p>记录 {items.length} 条审计事件</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>发生时间</th>
                <th>操作主体 (Actor)</th>
                <th>操作行为 (Action)</th>
                <th>执行结果 (Result)</th>
              </tr>
            </thead>
            <tbody>
              {items.map((a) => (
                <tr key={a.id}>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {new Date(a.created_at).toLocaleString()}
                  </td>
                  <td>
                    <span className="mono">{a.actor_id}</span>
                  </td>
                  <td>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                      <IconFileText size={15} style={{ color: 'var(--brand-600)' }} />
                      <span className="mono" style={{ fontWeight: 600 }}>{a.action}</span>
                    </div>
                  </td>
                  <td>
                    <span
                      className={`badge ${
                        a.result === 'SUCCESS' || a.result === 'ALLOW' || a.result === 'OK'
                          ? 'badge-ok'
                          : a.result === 'DENY' || a.result === 'FAIL'
                          ? 'badge-err'
                          : 'badge-warn'
                      }`}
                    >
                      {a.result}
                    </span>
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={4} className="empty-tip">暂无审计流水记录</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}

type SettingsResponse = {
  heartbeat_interval_sec?: number
  offline_after_sec?: number
  max_sessions_per_employee?: number
  note?: string
  [key: string]: unknown
}

export function SettingsPage() {
  const [data, setData] = useState<SettingsResponse | null>(null)
  const [totpEnabled, setTotpEnabled] = useState(false)
  const [totpSecret, setTotpSecret] = useState('')
  const [totpMsg, setTotpMsg] = useState('')
  const [loading, setLoading] = useState(false)
  const [showRaw, setShowRaw] = useState(false)

  const load = () => {
    setLoading(true)
    Promise.all([
      apiGet<SettingsResponse>('/settings').then(setData).catch(() => {}),
      apiGet<{ enabled: boolean }>('/auth/totp').then((res) => setTotpEnabled(!!res.enabled)).catch(() => {}),
    ]).finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
  }, [])

  const enrollTotp = async () => {
    setTotpMsg('')
    try {
      const res = await apiPost<{ secret: string }>('/auth/totp/enroll')
      setTotpSecret(res.secret)
      setTotpEnabled(true)
      setTotpMsg('TOTP 密钥申请成功！请在身份验证器 App 中完成添加绑定。')
    } catch (err) {
      alert(err instanceof Error ? err.message : '申请 TOTP 密钥失败')
    }
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>系统环境与架构参数配置 (Settings)</h1>
          <p>Control Plane 核心调度运行参数、通信超时窗口与系统基础设施基线</p>
        </div>
        <div style={{ display: 'flex', gap: '0.6rem' }}>
          <button type="button" className="btn-ghost" onClick={() => setShowRaw(!showRaw)}>
            {showRaw ? '切换结构化卡片视图' : '查看原始 JSON'}
          </button>
          <button type="button" className="btn-ghost" onClick={() => load()} disabled={loading}>
            <IconRefresh size={15} />
            <span>重新加载</span>
          </button>
        </div>
      </header>

      {/* 结构化卡片视图 */}
      {!showRaw ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
          {/* 第一组：调度策略与心跳探测 */}
          <div className="panel" style={{ margin: 0 }}>
            <div className="panel-header">
              <div>
                <h2>工作站心跳与调度可靠性参数 (Reliability & Scheduler)</h2>
                <p>保障 Workstation 计算节点健康感知、网络断线熔断与状态单调递增</p>
              </div>
              <span className="badge badge-ok">运行中生效</span>
            </div>

            <div className="settings-grid">
              <div className="setting-item">
                <div className="setting-title">心跳同步周期 (Heartbeat Interval)</div>
                <div className="setting-val-box">
                  <span className="setting-val">{data?.heartbeat_interval_sec ?? 5}</span>
                  <span className="setting-unit">秒 / 次</span>
                </div>
                <div className="setting-desc">工作站后台守护进程向中心服务器上报硬件负载与健康度的频率。</div>
              </div>

              <div className="setting-item">
                <div className="setting-title">离线判定窗口 (Offline Threshold)</div>
                <div className="setting-val-box">
                  <span className="setting-val" style={{ color: 'var(--warning)' }}>
                    {data?.offline_after_sec ?? 15}
                  </span>
                  <span className="setting-unit">秒超时</span>
                </div>
                <div className="setting-desc">连续未收到心跳达到此时间后，系统自动将节点置为 OFFLINE 并熔断派单。</div>
              </div>

              <div className="setting-item">
                <div className="setting-title">单员工活跃会话配额 (Max Sessions)</div>
                <div className="setting-val-box">
                  <span className="setting-val" style={{ color: 'var(--brand-600)' }}>
                    {data?.max_sessions_per_employee ?? 1}
                  </span>
                  <span className="setting-unit">会话 / 员工</span>
                </div>
                <div className="setting-desc">V1/V2 决策 Q-03 约定：每个数字员工同一时段保持单一活跃交互上下文。</div>
              </div>

              <div className="setting-item">
                <div className="setting-title">调度流转模式 (Scheduler Strategy)</div>
                <div className="setting-val-box">
                  <span style={{ fontSize: '1rem', fontWeight: 700, color: 'var(--text-primary)' }}>
                    资源感知限额排队
                  </span>
                </div>
                <div className="setting-desc">当节点 CPU / 内存达到超限阈值时，Job 保持排队并向管控台回显拒绝原因。</div>
              </div>
            </div>
          </div>

          {/* 第二组：基础设施与中间件拓扑 */}
          <div className="panel" style={{ margin: 0 }}>
            <div className="panel-header">
              <div>
                <h2>基础设施与服务技术栈基线 (Infrastructure Stack)</h2>
                <p>根据系统架构设计规范（§71、§72、§85）部署的基础组件状态</p>
              </div>
            </div>

            <div className="settings-grid">
              <div className="setting-item">
                <div className="setting-title">服务核心引擎 (Control Plane)</div>
                <div className="setting-text-val">Go 1.22+ 编译运行</div>
                <div className="setting-desc">提供 REST API (:8080) 与双向 mTLS gRPC 长连服务 (:9090)。</div>
              </div>

              <div className="setting-item">
                <div className="setting-title">主数据持久化 (PostgreSQL)</div>
                <div className="setting-text-val">PostgreSQL 16 (Alpine)</div>
                <div className="setting-desc">7 项架构迁移已执行就绪，承载用户、员工、任务与审计日志。</div>
              </div>

              <div className="setting-item">
                <div className="setting-title">事件总线与队列 (Event Bus)</div>
                <div className="setting-text-val">Go 进程内环形缓冲区总线</div>
                <div className="setting-desc">V1/V2 架构极简设计，免除外部 Redis 依赖，直接驱动 Admin SSE。</div>
              </div>

              <div className="setting-item">
                <div className="setting-title">管理后台 Web 托管 (Admin Web)</div>
                <div className="setting-text-val">React 19 + Nginx 1.27</div>
                <div className="setting-desc">位于本地端口 :8088，通过内置反向代理转发 /api/ 与 SSE 事件流。</div>
              </div>
            </div>
          </div>

          {/* 第三组：安全基线与凭证保全 */}
          <div className="panel" style={{ margin: 0 }}>
            <div className="panel-header">
              <div>
                <h2>安全基线与凭证脱敏策略 (Security & Compliance)</h2>
                <p>遵循企业级安全设计规范（§88），保障敏感资产无明文泄露风险</p>
              </div>
            </div>

            <div className="settings-grid">
              <div className="setting-item">
                <div className="setting-title">首次部署初始化向导 (First-Time Setup)</div>
                <div className="setting-text-val" style={{ color: 'var(--brand-600)' }}>
                  已完成部署 · 通道已锁定
                </div>
                <div className="setting-desc">系统超级管理员账号已在首次安装时创建，初始化设置接口已永久锁定以防重入。</div>
              </div>

              <div className="setting-item">
                <div className="setting-title">敏感凭证保护机制 (Secret Vault)</div>
                <div className="setting-text-val" style={{ color: 'var(--brand-600)' }}>
                  引用隔离与自动掩码
                </div>
                <div className="setting-desc">{data?.note || 'Secret 类配置仅保存加密引用，严格不在此处与前端明文回显。'}</div>
              </div>

              <div className="setting-item" style={{ gridColumn: '1 / -1', background: '#f8fafc', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: '1rem' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '0.5rem' }}>
                  <div>
                    <div className="setting-title" style={{ fontSize: '0.95rem', color: 'var(--text-primary)' }}>
                      管理员双因子安全认证 (TOTP 2FA)
                    </div>
                    <div className="setting-desc" style={{ marginTop: '0.2rem' }}>
                      启用后将在登录后台、执行高危审批、更新敏感配置时进行 6 位动态验证码鉴权保护。
                    </div>
                  </div>
                  <div>
                    {totpEnabled ? (
                      <span className="badge badge-ok" style={{ fontSize: '0.85rem', padding: '0.3rem 0.6rem' }}>
                        ✅ TOTP 双因子已生效
                      </span>
                    ) : (
                      <button type="button" className="btn btn-sm" onClick={() => void enrollTotp()}>
                        立即开通绑定 TOTP
                      </button>
                    )}
                  </div>
                </div>
                {totpSecret && (
                  <div style={{ marginTop: '0.85rem', padding: '0.85rem 1rem', background: '#ffffff', border: '1px solid #cbd5e1', borderRadius: '6px' }}>
                    <div style={{ fontWeight: 700, color: 'var(--brand-700)', marginBottom: '0.35rem' }}>
                      🔑 请在 Authenticator App 中添加新账号（密钥仅展示一次）:
                    </div>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginTop: '0.35rem' }}>
                      <code style={{ fontSize: '1rem', fontWeight: 700, letterSpacing: '0.1em', background: '#f1f5f9', padding: '0.3rem 0.6rem', borderRadius: '4px' }}>
                        {totpSecret}
                      </code>
                      <button
                        type="button"
                        className="btn-ghost btn-sm"
                        onClick={() => {
                          void navigator.clipboard.writeText(totpSecret)
                          setTotpMsg('密钥已复制到剪贴板！')
                        }}
                      >
                        复制密钥
                      </button>
                    </div>
                    {totpMsg && <p style={{ color: 'var(--brand-600)', fontSize: '0.82rem', margin: '0.4rem 0 0' }}>{totpMsg}</p>}
                    <p style={{ fontSize: '0.8rem', color: 'var(--text-muted)', margin: '0.4rem 0 0' }}>
                      支持 Google Authenticator、Microsoft Authenticator、1Password 等通用 TOTP 客户端。
                    </p>
                  </div>
                )}
              </div>

              <div className="setting-item">
                <div className="setting-title">通信加密与身份验签 (mTLS)</div>
                <div className="setting-text-val">TLS 1.3 + Dev CA 证书体系</div>
                <div className="setting-desc">工作站出站单向连接，私钥严禁上传中心，公钥由 Control Plane 统一认证。</div>
              </div>

              <div className="setting-item">
                <div className="setting-title">审计日志保全 (Audit Trail)</div>
                <div className="setting-text-val">只追加不可篡改审计流</div>
                <div className="setting-desc">全生命周期跟踪记录操作主体、判定策略、审批结果与时间戳。</div>
              </div>
            </div>
          </div>
        </div>
      ) : (
        /* 开发者原始 JSON 面板 */
        <div className="panel">
          <div className="panel-header">
            <div>
              <h2>Control Plane 原始配置回显 (Developer Raw JSON)</h2>
              <p>接口 GET /api/settings 返回的原始数据</p>
            </div>
          </div>
          <pre
            style={{
              background: '#0f172a',
              color: '#34d399',
              padding: '1.25rem',
              borderRadius: '10px',
              fontFamily: 'var(--font-mono)',
              fontSize: '0.85rem',
              lineHeight: 1.6,
              overflowX: 'auto',
            }}
          >
            {data ? JSON.stringify(data, null, 2) : '正在加载中…'}
          </pre>
        </div>
      )}
    </section>
  )
}

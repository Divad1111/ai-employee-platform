/**
 * 备份与容灾恢复中心 (Center Server Backup System)
 * 设计依据：docs/AI_Employee_Center_Server_Backup_Design.md
 */
import { FormEvent, useEffect, useState } from 'react'
import { apiPost } from '../../api/client'
import {
  type BackupDestination,
  type BackupOverview,
  type BackupPolicy,
  type BackupRun,
  type DestinationType,
  type RestoreJob,
  type VerificationReport,
  createBackupDestination,
  createBackupPolicy,
  deleteBackupDestination,
  deleteBackupPolicy,
  deleteBackupRun,
  disableBackupPolicy,
  enableBackupPolicy,
  getBackupOverview,
  listBackupDestinations,
  listBackupPolicies,
  listBackupRuns,
  listRestoreJobs,
  restoreBackupRun,
  runBackupPolicy,
  runManualBackup,
  testBackupDestination,
  updateBackupDestination,
  updateBackupPolicy,
  verifyBackupRun,
} from '../../api/backups'
import {
  IconCheckCircle,
  IconClock,
  IconDashboard,
  IconKey,
  IconPackage,
  IconRefresh,
  IconServer,
  IconShield,
  IconTrash,
  IconZap,
} from '../../components/Icons'
import { StatusBadge } from '../../components/StatusBadge'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { formatTime } from '../../lib/time'
import { usePerm } from '../../stores/permissions'

type Tab = 'overview' | 'policies' | 'destinations' | 'runs' | 'restore'

function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(2)} ${units[i]}`
}

function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

export function BackupsPage() {
  const { canAll } = usePerm()
  const canManage = canAll('backup.manage')
  const canCreate = canAll('backup.create')
  const canDest = canAll('backup.destination')
  const canVerify = canAll('backup.verify')
  const canDelete = canAll('backup.delete')
  const canRestore = canAll('backup.restore')

  const [tab, setTab] = useState<Tab>('overview')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [msg, setMsg] = useState('')

  // 数据集
  const [overview, setOverview] = useState<BackupOverview | null>(null)
  const [destinations, setDestinations] = useState<BackupDestination[]>([])
  const [policies, setPolicies] = useState<BackupPolicy[]>([])
  const [runs, setRuns] = useState<BackupRun[]>([])
  const [restoreJobs, setRestoreJobs] = useState<RestoreJob[]>([])

  // 弹窗状态
  const [destModalOpen, setDestModalOpen] = useState(false)
  const [editingDest, setEditingDest] = useState<BackupDestination | null>(null)
  const [destForm, setDestForm] = useState<{
    name: string
    type: DestinationType
    localPath: string
    s3Endpoint: string
    s3Region: string
    s3Bucket: string
    s3Prefix: string
    s3AccessKey: string
    s3SecretKey: string
    s3UseSSL: boolean
    s3PathStyle: boolean
    sftpHost: string
    sftpPort: number
    sftpUser: string
    sftpPass: string
    sftpKey: string
    sftpPath: string
    smbServer: string
    smbShare: string
    smbUser: string
    smbPass: string
    smbDomain: string
    smbPath: string
  }>({
    name: '',
    type: 'LOCAL',
    localPath: '/data/backups',
    s3Endpoint: '',
    s3Region: 'us-east-1',
    s3Bucket: '',
    s3Prefix: 'aie-backup',
    s3AccessKey: '',
    s3SecretKey: '',
    s3UseSSL: true,
    s3PathStyle: false,
    sftpHost: '',
    sftpPort: 22,
    sftpUser: '',
    sftpPass: '',
    sftpKey: '',
    sftpPath: '/backup',
    smbServer: '',
    smbShare: '',
    smbUser: '',
    smbPass: '',
    smbDomain: '',
    smbPath: '',
  })

  // 策略弹窗状态
  const [policyModalOpen, setPolicyModalOpen] = useState(false)
  const [editingPolicy, setEditingPolicy] = useState<BackupPolicy | null>(null)
  const [policyForm, setPolicyForm] = useState<{
    name: string
    scheduleType: 'MANUAL' | 'CRON'
    cronPreset: string
    cronExpression: string
    timezone: string
    retentionKeepLast: number
    encryptionEnabled: boolean // 是否加密开关
    compressionAlgorithm: 'zstd' | 'gzip'
    destinationIDs: string[]
  }>({
    name: '',
    scheduleType: 'CRON',
    cronPreset: 'daily_02',
    cronExpression: '0 2 * * *',
    timezone: 'Asia/Shanghai',
    retentionKeepLast: 7,
    encryptionEnabled: true,
    compressionAlgorithm: 'zstd',
    destinationIDs: [],
  })

  // 手动备份弹窗状态
  const [manualModalOpen, setManualModalOpen] = useState(false)
  const [manualDestIDs, setManualDestIDs] = useState<string[]>([])
  const [manualEncEnabled, setManualEncEnabled] = useState(true)

  // 校验报告弹窗
  const [verifyReport, setVerifyReport] = useState<VerificationReport | null>(null)
  const [verifyingID, setVerifyingID] = useState('')

  // 运行详情抽屉
  const [detailRun, setDetailRun] = useState<BackupRun | null>(null)

  // 恢复确认弹窗
  const [restoreConfirmRun, setRestoreConfirmRun] = useState<BackupRun | null>(null)
  const [restoreAdminPassword, setRestoreAdminPassword] = useState('')
  const [restoreTotpCode, setRestoreTotpCode] = useState('')
  const [restoreModalError, setRestoreModalError] = useState('')
  const [restoring, setRestoring] = useState(false)

  // 测试连接进行中状态
  const [testingDestID, setTestingDestID] = useState('')

  // 通用删除确认弹窗状态 (ConfirmDialog)
  const [deleteTarget, setDeleteTarget] = useState<{
    type: 'dest' | 'policy' | 'run'
    id: string
    title: string
    label: string
    meta?: string
  } | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)

  async function loadData() {
    setLoading(true)
    setError('')
    try {
      const [ov, destRes, polRes, runRes, restRes] = await Promise.all([
        getBackupOverview().catch(() => null),
        listBackupDestinations().catch(() => ({ items: [] })),
        listBackupPolicies().catch(() => ({ items: [] })),
        listBackupRuns({ limit: 50 }).catch(() => ({ items: [], total: 0 })),
        listRestoreJobs().catch(() => ({ items: [] })),
      ])
      setOverview(ov)
      setDestinations(Array.isArray(destRes?.items) ? destRes.items : [])
      setPolicies(Array.isArray(polRes?.items) ? polRes.items : [])
      setRuns(Array.isArray(runRes?.items) ? runRes.items : [])
      setRestoreJobs(Array.isArray(restRes?.items) ? restRes.items : [])
    } catch (e: any) {
      setError(e.message || '加载备份数据失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadData()
  }, [])

  function showMsg(text: string) {
    setMsg(text)
    setTimeout(() => setMsg(''), 4000)
  }

  // 选项卡切换统一逻辑：清空所有打开的弹窗与详情抽屉
  function handleTabChange(nextTab: Tab) {
    setTab(nextTab)
    setDetailRun(null)
    setDestModalOpen(false)
    setEditingDest(null)
    setPolicyModalOpen(false)
    setEditingPolicy(null)
    setManualModalOpen(false)
    setVerifyReport(null)
    setRestoreConfirmRun(null)
    setDeleteTarget(null)
  }

  // 测试存储目标连接
  async function handleTestDest(d: BackupDestination) {
    setTestingDestID(d.id)
    try {
      const res = await testBackupDestination(d.id)
      if (res.success) {
        showMsg(`[${d.name}] ${res.message || '连通性测试通过'}`)
      } else {
        setError(`[${d.name}] 测试失败: ${res.error}`)
      }
      void loadData()
    } catch (e: any) {
      setError(`测试发生异常: ${e.message}`)
    } finally {
      setTestingDestID('')
    }
  }

  // 启停策略
  async function handleTogglePolicy(p: BackupPolicy) {
    try {
      if (p.enabled) {
        await disableBackupPolicy(p.id)
        showMsg(`策略 [${p.name}] 已停用`)
      } else {
        await enableBackupPolicy(p.id)
        showMsg(`策略 [${p.name}] 已启用`)
      }
      void loadData()
    } catch (e: any) {
      setError(e.message)
    }
  }

  // 立即执行策略
  async function handleRunPolicy(p: BackupPolicy) {
    try {
      await runBackupPolicy(p.id)
      showMsg(`策略 [${p.name}] 备份任务已在后台拉起`)
      setTimeout(() => void loadData(), 1200)
    } catch (e: any) {
      setError(e.message)
    }
  }

  // 提交手动非绑定备份
  async function handleManualSubmit(e: FormEvent) {
    e.preventDefault()
    if (manualDestIDs.length === 0) {
      setError('请至少选择一个存储目标')
      return
    }
    try {
      await runManualBackup({
        destination_ids: manualDestIDs,
        encryption_enabled: manualEncEnabled,
      })
      setManualModalOpen(false)
      showMsg('手动备份任务已成功提交并在后台执行')
      setTimeout(() => void loadData(), 1000)
    } catch (e: any) {
      setError(e.message)
    }
  }

  // 校验备份
  async function handleVerify(r: BackupRun) {
    setVerifyingID(r.id)
    try {
      const res = await verifyBackupRun(r.id)
      setVerifyReport(res.report)
      if (res.passed) {
        showMsg(`备份 [${r.backup_id}] 完整性校验通过！`)
      } else {
        setError(`校验未通过: ${res.error}`)
      }
    } catch (e: any) {
      setError(`校验失败: ${e.message}`)
    } finally {
      setVerifyingID('')
    }
  }

  // 执行恢复
  async function handleRestoreSubmit() {
    if (!restoreConfirmRun) return
    setRestoring(true)
    setRestoreModalError('')
    try {
      // 若填写了二次鉴权密码，先请求 step-up 提权
      if (restoreAdminPassword.trim()) {
        await apiPost('/auth/step-up', {
          password: restoreAdminPassword.trim(),
          totp: restoreTotpCode.trim() || undefined,
        })
      }
      const job = await restoreBackupRun(restoreConfirmRun.id)
      showMsg(`容灾恢复任务已创建 (${job.id})，正在后台还原数据...`)
      setRestoreConfirmRun(null)
      setRestoreAdminPassword('')
      setRestoreTotpCode('')
      setTab('restore')
      setTimeout(() => void loadData(), 1500)
    } catch (e: any) {
      const errMsg = e.message || '触发恢复失败'
      if (errMsg.includes('step-up') || errMsg.includes('二次认证') || errMsg.includes('二次验证')) {
        setRestoreModalError('此操作属于高危容灾操作，请输入当前管理员登录密码完成二次鉴权 (Step-Up)')
      } else {
        setRestoreModalError(errMsg)
      }
    } finally {
      setRestoring(false)
    }
  }

  // 执行通用确认弹窗删除
  async function handleConfirmDelete() {
    if (!deleteTarget) return
    setDeleteBusy(true)
    try {
      if (deleteTarget.type === 'dest') {
        await deleteBackupDestination(deleteTarget.id)
        showMsg(`存储目标 [${deleteTarget.label}] 已成功删除`)
      } else if (deleteTarget.type === 'policy') {
        await deleteBackupPolicy(deleteTarget.id)
        showMsg(`备份策略 [${deleteTarget.label}] 已成功删除`)
      } else if (deleteTarget.type === 'run') {
        await deleteBackupRun(deleteTarget.id)
        showMsg(`备份快照记录 [${deleteTarget.label}] 及其物理文件已删除`)
      }
      setDeleteTarget(null)
      void loadData()
    } catch (e: any) {
      setError(e.message || '删除失败')
    } finally {
      setDeleteBusy(false)
    }
  }

  // 保存存储目标
  async function handleDestSubmit(e: FormEvent) {
    e.preventDefault()
    let cfg: any = {}
    if (destForm.type === 'LOCAL') {
      cfg = { path: destForm.localPath }
    } else if (destForm.type === 'S3') {
      cfg = {
        endpoint: destForm.s3Endpoint,
        region: destForm.s3Region,
        bucket: destForm.s3Bucket,
        prefix: destForm.s3Prefix,
        access_key: destForm.s3AccessKey,
        secret_key: destForm.s3SecretKey,
        use_ssl: destForm.s3UseSSL,
        path_style: destForm.s3PathStyle,
      }
    } else if (destForm.type === 'SFTP') {
      cfg = {
        host: destForm.sftpHost,
        port: destForm.sftpPort,
        username: destForm.sftpUser,
        password: destForm.sftpPass,
        private_key: destForm.sftpKey,
        remote_path: destForm.sftpPath,
      }
    } else if (destForm.type === 'SMB') {
      cfg = {
        server: destForm.smbServer,
        share: destForm.smbShare,
        username: destForm.smbUser,
        password: destForm.smbPass,
        domain: destForm.smbDomain,
        remote_path: destForm.smbPath,
      }
    }
    // 检查重复存储目标（类型与路径一致）
    const duplicate = destinations.find((d) => {
      if (editingDest && d.id === editingDest.id) return false
      if (d.type !== destForm.type) return false
      const c = (d.config as any) || {}
      if (destForm.type === 'LOCAL') {
        return (c.path || '/data/backups').trim() === destForm.localPath.trim()
      }
      if (destForm.type === 'S3') {
        return (
          (c.endpoint || '').trim().toLowerCase() === destForm.s3Endpoint.trim().toLowerCase() &&
          (c.bucket || '').trim() === destForm.s3Bucket.trim() &&
          (c.prefix || '').trim() === destForm.s3Prefix.trim()
        )
      }
      if (destForm.type === 'SFTP') {
        return (
          (c.host || '').trim().toLowerCase() === destForm.sftpHost.trim().toLowerCase() &&
          Number(c.port || 22) === Number(destForm.sftpPort || 22) &&
          (c.remote_path || '').trim() === destForm.sftpPath.trim()
        )
      }
      if (destForm.type === 'SMB') {
        return (
          (c.server || '').trim().toLowerCase() === destForm.smbServer.trim().toLowerCase() &&
          (c.share || '').trim() === destForm.smbShare.trim() &&
          (c.remote_path || '').trim() === destForm.smbPath.trim()
        )
      }
      return false
    })
    if (duplicate) {
      setError(`已存在相同类型且路径完全相同的存储目标「${duplicate.name}」，请勿重复创建`)
      return
    }

    try {
      if (editingDest) {
        await updateBackupDestination(editingDest.id, {
          name: destForm.name,
          type: destForm.type,
          config: cfg,
        })
        showMsg(`存储目标 [${destForm.name}] 更新成功`)
      } else {
        await createBackupDestination({
          name: destForm.name,
          type: destForm.type,
          config: cfg,
        })
        showMsg(`存储目标 [${destForm.name}] 创建成功`)
      }
      setDestModalOpen(false)
      setEditingDest(null)
      void loadData()
    } catch (e: any) {
      setError(e.message)
    }
  }

  // 保存策略
  async function handlePolicySubmit(e: FormEvent) {
    e.preventDefault()
    if (policyForm.destinationIDs.length === 0) {
      setError('请至少为策略选择一个存储目标')
      return
    }

    let expr = policyForm.cronExpression
    if (policyForm.scheduleType === 'CRON') {
      if (policyForm.cronPreset === 'daily_02') expr = '0 2 * * *'
      else if (policyForm.cronPreset === 'daily_04') expr = '0 4 * * *'
      else if (policyForm.cronPreset === 'weekly_sun') expr = '0 3 * * 0'
    }

    const payload: Partial<BackupPolicy> = {
      name: policyForm.name,
      scope: 'CENTER_FULL',
      schedule_type: policyForm.scheduleType,
      cron_expression: expr,
      timezone: policyForm.timezone,
      retention_keep_last: Number(policyForm.retentionKeepLast),
      encryption_enabled: policyForm.encryptionEnabled,
      compression_algorithm: policyForm.compressionAlgorithm,
      destination_ids: policyForm.destinationIDs,
    }

    try {
      if (editingPolicy) {
        await updateBackupPolicy(editingPolicy.id, payload)
        showMsg(`备份策略 [${policyForm.name}] 已更新`)
      } else {
        await createBackupPolicy(payload)
        showMsg(`备份策略 [${policyForm.name}] 创建成功`)
      }
      setPolicyModalOpen(false)
      setEditingPolicy(null)
      void loadData()
    } catch (e: any) {
      setError(e.message)
    }
  }

  // 打开编辑存储目标
  function openEditDest(d: BackupDestination) {
    setEditingDest(d)
    const cfg = d.config || {}
    setDestForm({
      name: d.name,
      type: d.type,
      localPath: cfg.path || '/data/backups',
      s3Endpoint: cfg.endpoint || '',
      s3Region: cfg.region || 'us-east-1',
      s3Bucket: cfg.bucket || '',
      s3Prefix: cfg.prefix || 'aie-backup',
      s3AccessKey: cfg.access_key || '',
      s3SecretKey: '',
      s3UseSSL: cfg.use_ssl ?? true,
      s3PathStyle: cfg.path_style ?? false,
      sftpHost: cfg.host || '',
      sftpPort: cfg.port || 22,
      sftpUser: cfg.username || '',
      sftpPass: '',
      sftpKey: '',
      sftpPath: cfg.remote_path || '/backup',
      smbServer: cfg.server || '',
      smbShare: cfg.share || '',
      smbUser: cfg.username || '',
      smbPass: '',
      smbDomain: cfg.domain || '',
      smbPath: cfg.remote_path || '',
    })
    setDestModalOpen(true)
  }

  // 打开编辑策略
  function openEditPolicy(p: BackupPolicy) {
    setEditingPolicy(p)
    let preset = 'custom'
    if (p.cron_expression === '0 2 * * *') preset = 'daily_02'
    else if (p.cron_expression === '0 4 * * *') preset = 'daily_04'
    else if (p.cron_expression === '0 3 * * 0') preset = 'weekly_sun'

    setPolicyForm({
      name: p.name,
      scheduleType: p.schedule_type,
      cronPreset: preset,
      cronExpression: p.cron_expression,
      timezone: p.timezone || 'Asia/Shanghai',
      retentionKeepLast: p.retention_keep_last || 7,
      encryptionEnabled: p.encryption_enabled,
      compressionAlgorithm: p.compression_algorithm || 'zstd',
      destinationIDs: p.destination_ids || [],
    })
    setPolicyModalOpen(true)
  }

  return (
    <div className="content">
      {/* 顶部标题栏 */}
      <div className="page-header">
        <div>
          <h1 style={{ display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
            <IconServer size={26} style={{ color: 'var(--brand-500)' }} />
            备份与容灾恢复 (Backup System)
          </h1>
          <p>
            中心服务器全量快照备份、本地与多云（S3/SFTP/SMB）容灾分发、可配置加密策略与一键安全回滚恢复
          </p>
        </div>
        <div style={{ display: 'flex', gap: '0.6rem' }}>
          <button
            type="button"
            className="btn btn-secondary"
            onClick={() => void loadData()}
            disabled={loading}
          >
            <IconRefresh size={16} className={loading ? 'spin' : ''} />
            刷新
          </button>
          {canCreate && (
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => {
                setManualDestIDs(destinations.filter((d) => d.enabled).map((d) => d.id))
                setManualEncEnabled(true)
                setManualModalOpen(true)
              }}
            >
              <IconZap size={16} />
              立即手动备份
            </button>
          )}
        </div>
      </div>

      {/* 提示消息 */}
      {error && (
        <div className="panel" style={{ borderLeft: '4px solid var(--danger)', marginBottom: '1rem', color: 'var(--danger)' }}>
          {error}
        </div>
      )}
      {msg && (
        <div className="panel" style={{ borderLeft: '4px solid var(--success)', marginBottom: '1rem', color: 'var(--success)' }}>
          {msg}
        </div>
      )}

      {/* 顶部指标统计看板 */}
      <div className="stat-grid">
        <div className="stat-card">
          <div className="stat-label">备份执行总次数</div>
          <div className="stat-value">{overview?.total_runs ?? 0}</div>
          <div className="stat-sub" style={{ display: 'flex', gap: '0.5rem' }}>
            <span style={{ color: 'var(--success)' }}>成功: {overview?.success_runs ?? 0}</span>
            <span style={{ color: 'var(--danger)' }}>失败: {overview?.failed_runs ?? 0}</span>
          </div>
        </div>

        <div className="stat-card">
          <div className="stat-label">备份成功率</div>
          <div className="stat-value" style={{ color: 'var(--brand-600)' }}>
            {overview && overview.total_runs > 0
              ? `${Math.round(((overview.success_runs + overview.partial_runs) / overview.total_runs) * 100)}%`
              : '100%'}
          </div>
          <div className="stat-sub">多目标容灾保障</div>
        </div>

        <div className="stat-card">
          <div className="stat-label">活跃备份策略</div>
          <div className="stat-value">{overview?.active_policies ?? 0}</div>
          <div className="stat-sub">共配置 {overview?.total_policies ?? 0} 个策略</div>
        </div>

        <div className="stat-card">
          <div className="stat-label">存储目标数</div>
          <div className="stat-value">{overview?.total_destinations ?? 0}</div>
          <div className="stat-sub">已连接 Local / S3 / SFTP / SMB</div>
        </div>

        <div className="stat-card">
          <div className="stat-label">累计快照数据量</div>
          <div className="stat-value" style={{ fontSize: '1.65rem' }}>
            {formatBytes(overview?.total_artifact_size ?? 0)}
          </div>
          <div className="stat-sub">Zstd 高速压缩</div>
        </div>

        <div className="stat-card">
          <div className="stat-label">最近成功备份</div>
          <div className="stat-value" style={{ fontSize: '1.1rem', marginTop: '0.8rem' }}>
            {overview?.last_success_run_at ? formatTime(overview.last_success_run_at) : '暂无记录'}
          </div>
          <div className="stat-sub">
            {overview?.next_scheduled_at ? `下次预计: ${formatTime(overview.next_scheduled_at)}` : '未配置自动调度'}
          </div>
        </div>
      </div>

      {/* 现代风格 Segmented Tabs 导航 */}
      <div className="tab-nav" style={{ marginBottom: '1.25rem' }}>
        <button
          type="button"
          className={`tab-btn ${tab === 'overview' ? 'active' : ''}`}
          onClick={() => handleTabChange('overview')}
        >
          <IconDashboard size={16} />
          运行总览
        </button>
        <button
          type="button"
          className={`tab-btn ${tab === 'policies' ? 'active' : ''}`}
          onClick={() => handleTabChange('policies')}
        >
          <IconClock size={16} />
          备份策略 ({(policies || []).length})
        </button>
        <button
          type="button"
          className={`tab-btn ${tab === 'destinations' ? 'active' : ''}`}
          onClick={() => handleTabChange('destinations')}
        >
          <IconServer size={16} />
          存储目标 ({(destinations || []).length})
        </button>
        <button
          type="button"
          className={`tab-btn ${tab === 'runs' ? 'active' : ''}`}
          onClick={() => handleTabChange('runs')}
        >
          <IconPackage size={16} />
          备份历史记录 ({(runs || []).length})
        </button>
        <button
          type="button"
          className={`tab-btn ${tab === 'restore' ? 'active' : ''}`}
          onClick={() => handleTabChange('restore')}
        >
          <IconShield size={16} />
          容灾恢复任务 ({(restoreJobs || []).length})
        </button>
      </div>

      {/* TAB 1: 运行总览 */}
      {tab === 'overview' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
          {/* 健康卡片 */}
          <div
            className="panel"
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              background: overview?.last_run_status === 'FAILED' ? 'var(--danger-bg)' : 'var(--brand-50)',
              borderColor: overview?.last_run_status === 'FAILED' ? 'var(--danger-border)' : 'var(--brand-100)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
              <IconCheckCircle
                size={32}
                style={{
                  color: overview?.last_run_status === 'FAILED' ? 'var(--danger)' : 'var(--brand-600)',
                }}
              />
              <div>
                <h3 style={{ margin: 0, fontSize: '1.1rem', color: 'var(--text-primary)' }}>
                  {overview?.last_run_status === 'FAILED' ? '系统最近一次备份发生异常' : 'Center Server 数据防护状态就绪'}
                </h3>
                <p style={{ margin: '0.25rem 0 0', color: 'var(--text-secondary)', fontSize: '0.85rem' }}>
                  {overview?.last_success_run_at
                    ? `最近一次成功备份生成于 ${formatTime(overview.last_success_run_at)}，快照文件已受校验保护。`
                    : '尚未生成任何成功备份，建议立即创建策略或发起手动全量快照。'}
                </p>
              </div>
            </div>
            {canCreate && (
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => {
                  setManualDestIDs(destinations.filter((d) => d.enabled).map((d) => d.id))
                  setManualModalOpen(true)
                }}
              >
                立即执行一次全量备份
              </button>
            )}
          </div>

          {/* 近期备份执行 */}
          <div className="panel">
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
              <h3 style={{ margin: 0, fontSize: '1.05rem' }}>近期备份快照</h3>
              <button type="button" className="btn btn-sm btn-outline" onClick={() => setTab('runs')}>
                查看全部记录 →
              </button>
            </div>
            <div className="table-wrapper">
              <table className="table">
                <thead>
                  <tr>
                    <th>备份标识 (ID)</th>
                    <th>所属策略</th>
                    <th>生成时间</th>
                    <th>大小</th>
                    <th>耗时</th>
                    <th>加密状态</th>
                    <th>状态</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {runs.slice(0, 5).map((r) => (
                    <tr key={r.id}>
                      <td style={{ fontFamily: 'var(--font-mono)', fontWeight: 600 }}>{r.backup_id}</td>
                      <td>
                        {r.trigger_type === 'EMERGENCY' ? (
                          <span className="badge" style={{ background: '#fef3c7', color: '#b45309', border: '1px solid #fcd34d' }}>
                            🛡️ 还原应急快照
                          </span>
                        ) : (
                          r.policy_name || '手动即时备份'
                        )}
                      </td>
                      <td>{formatTime(r.created_at)}</td>
                      <td>{formatBytes(r.artifact_size)}</td>
                      <td>{formatDuration(r.duration_ms)}</td>
                      <td>
                        {r.manifest?.encryption?.algorithm === 'NONE' || !r.manifest?.encryption ? (
                          <span className="badge" style={{ background: '#fef3c7', color: '#b45309' }}>未加密 (压缩包)</span>
                        ) : (
                          <span className="badge badge-success">已加密 (AES-256)</span>
                        )}
                      </td>
                      <td>
                        <StatusBadge status={r.status} />
                      </td>
                      <td>
                        <button type="button" className="btn btn-sm btn-outline" onClick={() => setDetailRun(r)}>
                          详情
                        </button>
                      </td>
                    </tr>
                  ))}
                  {runs.length === 0 && (
                    <tr>
                      <td colSpan={8} style={{ textAlign: 'center', padding: '2rem', color: 'var(--text-muted)' }}>
                        暂无备份运行记录
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* TAB 2: 备份策略 */}
      {tab === 'policies' && (
        <div className="panel">
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
            <div>
              <h3 style={{ margin: 0, fontSize: '1.1rem' }}>定时备份策略</h3>
              <p style={{ margin: '0.2rem 0 0', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                按计划全量备份 Center Server 数据库、CA 证书私钥、机密保管库与任务制品
              </p>
            </div>
            {canManage && (
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => {
                  setEditingPolicy(null)
                  setPolicyForm({
                    name: '',
                    scheduleType: 'CRON',
                    cronPreset: 'daily_02',
                    cronExpression: '0 2 * * *',
                    timezone: 'Asia/Shanghai',
                    retentionKeepLast: 7,
                    encryptionEnabled: true,
                    compressionAlgorithm: 'zstd',
                    destinationIDs: destinations.filter((d) => d.enabled).map((d) => d.id),
                  })
                  setPolicyModalOpen(true)
                }}
              >
                + 新建备份策略
              </button>
            )}
          </div>

          <div className="table-wrapper">
            <table className="table">
              <thead>
                <tr>
                  <th>策略名称</th>
                  <th>状态</th>
                  <th>调度周期</th>
                  <th>关联存储目标</th>
                  <th>保留策略</th>
                  <th>加密设置</th>
                  <th>上次执行</th>
                  <th>下次执行</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {policies.map((p) => (
                  <tr key={p.id}>
                    <td style={{ fontWeight: 600 }}>{p.name}</td>
                    <td>
                      {canManage ? (
                        <button
                          type="button"
                          className={`btn btn-sm ${p.enabled ? 'btn-primary' : 'btn-outline'}`}
                          onClick={() => void handleTogglePolicy(p)}
                          style={{ padding: '0.2rem 0.5rem', fontSize: '0.75rem' }}
                        >
                          {p.enabled ? '已启用' : '已停用'}
                        </button>
                      ) : (
                        <span className={`badge ${p.enabled ? 'badge-success' : 'badge-warning'}`}>
                          {p.enabled ? '已启用' : '已停用'}
                        </span>
                      )}
                    </td>
                    <td>
                      <div style={{ fontFamily: 'var(--font-mono)', fontSize: '0.85rem' }}>{p.cron_expression}</div>
                      <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>{p.timezone}</div>
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: '0.3rem', flexWrap: 'wrap' }}>
                        {p.destination_ids && p.destination_ids.length > 0 ? (
                          p.destination_ids.map((dID) => {
                            const found = destinations.find((d) => d.id === dID)
                            return (
                              <span key={dID} className="badge" style={{ background: '#f1f5f9', color: '#475569' }}>
                                {found ? found.name : dID}
                              </span>
                            )
                          })
                        ) : (
                          <span style={{ color: 'var(--danger)', fontSize: '0.75rem' }}>未绑定目标</span>
                        )}
                      </div>
                    </td>
                    <td>保留最近 {p.retention_keep_last} 次</td>
                    <td>
                      {/* 用户特殊要求的加密开关状态展示 */}
                      {p.encryption_enabled ? (
                        <span className="badge badge-success" title="全包 AES-256-GCM 加密保护">
                          已加密 (AES-256)
                        </span>
                      ) : (
                        <span
                          className="badge"
                          style={{ background: '#fef3c7', color: '#b45309' }}
                          title="用户设置：不加密，生成标准压缩归档"
                        >
                          未加密 (明文归档)
                        </span>
                      )}
                    </td>
                    <td>
                      <div>{p.last_run_at ? formatTime(p.last_run_at) : '—'}</div>
                      {p.last_run_status && <StatusBadge status={p.last_run_status} />}
                    </td>
                    <td style={{ fontSize: '0.82rem' }}>{p.next_run_at ? formatTime(p.next_run_at) : '—'}</td>
                    <td>
                      <div style={{ display: 'flex', gap: '0.35rem' }}>
                        {canCreate && (
                          <button
                            type="button"
                            className="btn btn-sm btn-outline"
                            title="立即执行此策略"
                            onClick={() => void handleRunPolicy(p)}
                          >
                            执行
                          </button>
                        )}
                        {canManage && (
                          <>
                            <button
                              type="button"
                              className="btn btn-sm btn-outline"
                              onClick={() => openEditPolicy(p)}
                            >
                              编辑
                            </button>
                            <button
                              type="button"
                              className="btn btn-sm btn-danger"
                              onClick={() => {
                                setDeleteTarget({
                                  type: 'policy',
                                  id: p.id,
                                  title: '删除备份策略',
                                  label: p.name,
                                  meta: `Cron 周期: ${p.cron_expression}`,
                                })
                              }}
                            >
                              删除
                            </button>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
                {policies.length === 0 && (
                  <tr>
                    <td colSpan={9} style={{ textAlign: 'center', padding: '2rem', color: 'var(--text-muted)' }}>
                      暂无备份策略，点击右上角新建策略
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* TAB 3: 存储目标 */}
      {tab === 'destinations' && (
        <div className="panel">
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
            <div>
              <h3 style={{ margin: 0, fontSize: '1.1rem' }}>备份存储目标 (Destinations)</h3>
              <p style={{ margin: '0.2rem 0 0', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                配置并接入本地存储或远程多云对象存储（S3/MinIO、SFTP、SMB 共享）
              </p>
            </div>
            {canDest && (
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => {
                  setEditingDest(null)
                  setDestForm({
                    name: '',
                    type: 'LOCAL',
                    localPath: '/data/backups',
                    s3Endpoint: '',
                    s3Region: 'us-east-1',
                    s3Bucket: '',
                    s3Prefix: 'aie-backup',
                    s3AccessKey: '',
                    s3SecretKey: '',
                    s3UseSSL: true,
                    s3PathStyle: false,
                    sftpHost: '',
                    sftpPort: 22,
                    sftpUser: '',
                    sftpPass: '',
                    sftpKey: '',
                    sftpPath: '/backup',
                    smbServer: '',
                    smbShare: '',
                    smbUser: '',
                    smbPass: '',
                    smbDomain: '',
                    smbPath: '',
                  })
                  setDestModalOpen(true)
                }}
              >
                + 添加存储目标
              </button>
            )}
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))', gap: '1rem' }}>
            {destinations.map((d) => (
              <div
                key={d.id}
                style={{
                  border: '1px solid var(--border-subtle)',
                  borderRadius: 'var(--radius-md)',
                  padding: '1.25rem',
                  background: 'var(--panel-bg)',
                  display: 'flex',
                  flexDirection: 'column',
                  justifyContent: 'space-between',
                  boxShadow: 'var(--shadow-sm)',
                }}
              >
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.6rem' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                      <span className="badge badge-info" style={{ fontWeight: 700 }}>{d.type}</span>
                      <strong style={{ fontSize: '1.05rem' }}>{d.name}</strong>
                    </div>
                    <span className={`badge ${d.enabled ? 'badge-success' : 'badge-warning'}`}>
                      {d.enabled ? '已启用' : '已停用'}
                    </span>
                  </div>

                  <div style={{ fontSize: '0.85rem', color: 'var(--text-secondary)', marginBottom: '0.8rem', wordBreak: 'break-all' }}>
                    {d.type === 'LOCAL' && <div>路径: <code>{d.config?.path || '/data/backups'}</code></div>}
                    {d.type === 'S3' && (
                      <div>
                        Endpoint: <code>{d.config?.endpoint || '—'}</code>
                        <br />
                        Bucket: <code>{d.config?.bucket || '—'}</code>
                      </div>
                    )}
                    {d.type === 'SFTP' && (
                      <div>
                        Host: <code>{d.config?.host}:{d.config?.port || 22}</code>
                        <br />
                        User: <code>{d.config?.username}</code>
                      </div>
                    )}
                    {d.type === 'SMB' && (
                      <div>
                        Server: <code>{d.config?.server}</code>
                        <br />
                        Share: <code>{d.config?.share}</code>
                      </div>
                    )}
                  </div>

                  <div style={{ fontSize: '0.78rem', color: 'var(--text-muted)', marginBottom: '1rem' }}>
                    <div>
                      最后连通测试:{' '}
                      {d.last_test_at ? (
                        <span style={{ color: d.last_test_status === 'SUCCESS' ? 'var(--success)' : 'var(--danger)' }}>
                          {formatTime(d.last_test_at)} ({d.last_test_status})
                        </span>
                      ) : (
                        '未测试'
                      )}
                    </div>
                    {d.last_test_message && (
                      <div style={{ marginTop: '0.2rem', color: 'var(--text-muted)' }}>
                        {d.last_test_message}
                      </div>
                    )}
                  </div>
                </div>

                <div style={{ display: 'flex', gap: '0.4rem', borderTop: '1px solid var(--border-subtle)', paddingTop: '0.8rem' }}>
                  {canDest && (
                    <button
                      type="button"
                      className="btn btn-sm btn-outline"
                      onClick={() => void handleTestDest(d)}
                      disabled={testingDestID === d.id}
                      style={{ flex: 1 }}
                    >
                      <IconZap size={14} className={testingDestID === d.id ? 'spin' : ''} />
                      {testingDestID === d.id ? '测试中...' : '测试连接'}
                    </button>
                  )}
                  {canDest && (
                    <>
                      <button
                        type="button"
                        className="btn btn-sm btn-outline"
                        onClick={() => openEditDest(d)}
                      >
                        编辑
                      </button>
                      <button
                        type="button"
                        className="btn btn-sm btn-danger"
                        title="删除存储目标"
                        onClick={() => {
                          setDeleteTarget({
                            type: 'dest',
                            id: d.id,
                            title: '删除存储目标',
                            label: d.name,
                            meta: `协议类型: ${d.type}`,
                          })
                        }}
                      >
                        <IconTrash size={14} />
                      </button>
                    </>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* TAB 4: 备份历史记录 */}
      {tab === 'runs' && (
        <div className="panel">
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
            <div>
              <h3 style={{ margin: 0, fontSize: '1.1rem' }}>全量备份执行记录</h3>
              <p style={{ margin: '0.2rem 0 0', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                查看每次备份状态、多目标上传详情、SHA-256 校验和与灾难恢复入口
              </p>
            </div>
            {canCreate && (
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => {
                  setManualDestIDs(destinations.filter((d) => d.enabled).map((d) => d.id))
                  setManualModalOpen(true)
                }}
              >
                + 立即备份
              </button>
            )}
          </div>

          <div className="table-wrapper">
            <table className="table">
              <thead>
                <tr>
                  <th>备份唯一标识</th>
                  <th>策略 / 来源</th>
                  <th>时间</th>
                  <th>耗时</th>
                  <th>大小</th>
                  <th>加密算法</th>
                  <th>目标上传明细</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((r) => (
                  <tr key={r.id}>
                    <td style={{ fontFamily: 'var(--font-mono)', fontWeight: 600 }}>{r.backup_id}</td>
                    <td>
                      <div>{r.policy_name || '手动备份'}</div>
                      <div style={{ fontSize: '0.72rem', color: 'var(--text-muted)' }}>
                        {r.trigger_type === 'EMERGENCY' ? (
                          <span className="badge" style={{ background: '#fef3c7', color: '#b45309', border: '1px solid #fcd34d', fontSize: '0.7rem' }}>
                            🛡️ 还原应急快照
                          </span>
                        ) : r.trigger_type === 'SCHEDULED' ? (
                          '定时调度'
                        ) : (
                          '人工触发'
                        )}
                      </div>
                    </td>
                    <td>{formatTime(r.created_at)}</td>
                    <td>{formatDuration(r.duration_ms)}</td>
                    <td>{formatBytes(r.artifact_size)}</td>
                    <td>
                      {r.manifest?.encryption?.algorithm === 'NONE' || !r.manifest?.encryption ? (
                        <span className="badge" style={{ background: '#fef3c7', color: '#b45309' }}>未加密 (明文压缩)</span>
                      ) : (
                        <span className="badge badge-success">AES-256-GCM</span>
                      )}
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: '0.3rem', flexWrap: 'wrap' }}>
                        {r.destinations && r.destinations.length > 0 ? (
                          r.destinations.map((rd) => (
                            <span
                              key={rd.id}
                              className={`badge ${rd.status === 'SUCCESS' ? 'badge-success' : 'badge-danger'}`}
                              title={rd.error_message || rd.remote_path}
                            >
                              {rd.destination_type || 'DEST'}: {rd.status}
                            </span>
                          ))
                        ) : (
                          <span style={{ color: 'var(--text-muted)', fontSize: '0.75rem' }}>—</span>
                        )}
                      </div>
                    </td>
                    <td>
                      <StatusBadge status={r.status} />
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: '0.3rem' }}>
                        <button
                          type="button"
                          className="btn btn-sm btn-outline"
                          onClick={() => setDetailRun(r)}
                        >
                          详情
                        </button>
                        {canVerify && r.status === 'SUCCESS' && (
                          <button
                            type="button"
                            className="btn btn-sm btn-outline"
                            onClick={() => void handleVerify(r)}
                            disabled={verifyingID === r.id}
                            title="流式下载并全面校验 SHA-256 与文件结构"
                          >
                            {verifyingID === r.id ? '校验中' : '校验'}
                          </button>
                        )}
                        {canRestore && r.status === 'SUCCESS' && (
                          <button
                            type="button"
                            className="btn btn-sm"
                            style={{ background: 'var(--brand-50)', color: 'var(--brand-700)', borderColor: 'var(--brand-200)' }}
                            onClick={() => setRestoreConfirmRun(r)}
                            title="以此备份还原系统"
                          >
                            恢复
                          </button>
                        )}
                        {canDelete && (
                          <button
                            type="button"
                            className="btn btn-sm btn-danger"
                            onClick={() => {
                              setDeleteTarget({
                                type: 'run',
                                id: r.id,
                                title: '删除备份快照记录',
                                label: r.backup_id,
                                meta: `归档体积: ${formatBytes(r.artifact_size)} · 创建时间: ${formatTime(r.created_at)}`,
                              })
                            }}
                            title="删除此备份及物理文件"
                          >
                            删除
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
                {runs.length === 0 && (
                  <tr>
                    <td colSpan={9} style={{ textAlign: 'center', padding: '2rem', color: 'var(--text-muted)' }}>
                      暂无备份历史记录
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* TAB 5: 容灾恢复任务 */}
      {tab === 'restore' && (
        <div className="panel">
          <div style={{ marginBottom: '1.25rem' }}>
            <h3 style={{ margin: 0, fontSize: '1.1rem' }}>容灾恢复执行审计与任务</h3>
            <p style={{ margin: '0.2rem 0 0', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
              系统恢复记录与自动生成的 Emergency Backup 紧急应急快照追踪
            </p>
          </div>

          <div
            style={{
              padding: '1rem',
              borderRadius: 'var(--radius-md)',
              background: '#fffbeb',
              border: '1px solid #fde68a',
              marginBottom: '1.25rem',
              fontSize: '0.85rem',
              color: '#92400e',
            }}
          >
            <strong>高危操作防护：</strong> 恢复系统前，平台会强制自动打一份当前状态的 Emergency
            Backup 应急快照，确保任何因意外中断或版本不适导致的问题均可回滚。
          </div>

          <div className="table-wrapper">
            <table className="table">
              <thead>
                <tr>
                  <th>恢复任务 ID</th>
                  <th>恢复源备份</th>
                  <th>系统应急备份快照</th>
                  <th>执行人</th>
                  <th>开始时间</th>
                  <th>完成时间</th>
                  <th>状态</th>
                  <th>错误信息</th>
                </tr>
              </thead>
              <tbody>
                {restoreJobs.map((j) => (
                  <tr key={j.id}>
                    <td style={{ fontFamily: 'var(--font-mono)', fontWeight: 600 }}>{j.id}</td>
                    <td style={{ fontFamily: 'var(--font-mono)' }}>{j.backup_run_id}</td>
                    <td style={{ fontFamily: 'var(--font-mono)' }}>
                      {j.emergency_backup_run_id ? (
                        <button
                          type="button"
                          className="btn btn-sm btn-outline"
                          style={{
                            fontSize: '0.75rem',
                            padding: '0.2rem 0.5rem',
                            display: 'inline-flex',
                            alignItems: 'center',
                            gap: '0.3rem',
                            color: 'var(--brand-700)',
                            background: 'var(--brand-50)',
                          }}
                          onClick={() => {
                            const found = runs.find(
                              (r) =>
                                r.id === j.emergency_backup_run_id ||
                                r.backup_id === j.emergency_backup_run_id ||
                                'run-' + r.backup_id === j.emergency_backup_run_id,
                            )
                            if (found) {
                              setDetailRun(found)
                            } else {
                              showMsg(`应急快照 ID: ${j.emergency_backup_run_id}`)
                            }
                          }}
                          title="查看该还原任务自动生成的应急备份详情"
                        >
                          🛡️ 查看应急快照
                        </button>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td>{j.created_by || 'system'}</td>
                    <td>{j.started_at ? formatTime(j.started_at) : '—'}</td>
                    <td>{j.completed_at ? formatTime(j.completed_at) : '—'}</td>
                    <td>
                      <StatusBadge status={j.status} />
                    </td>
                    <td style={{ color: 'var(--danger)', fontSize: '0.82rem' }}>{j.error_message || '—'}</td>
                  </tr>
                ))}
                {restoreJobs.length === 0 && (
                  <tr>
                    <td colSpan={8} style={{ textAlign: 'center', padding: '2rem', color: 'var(--text-muted)' }}>
                      暂无容灾恢复记录
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* 弹窗 1: 创建/编辑存储目标 */}
      {destModalOpen && (
        <div className="modal-backdrop" onClick={() => setDestModalOpen(false)}>
          <div className="modal-card" style={{ width: '680px', maxWidth: '92vw' }} onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>{editingDest ? '编辑存储目标' : '添加备份存储目标'}</h3>
              <button type="button" className="modal-close-btn" onClick={() => setDestModalOpen(false)}>
                ✕
              </button>
            </div>
            <form onSubmit={handleDestSubmit} style={{ display: 'flex', flexDirection: 'column', flex: 1, minHeight: 0 }}>
              <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '1rem', maxHeight: '72vh', overflowY: 'auto' }}>
                <div>
                  <label className="form-label">目标名称 *</label>
                  <input
                    type="text"
                    className="input"
                    placeholder="例如：AWS-S3-Production 或 本地-NAS备份"
                    value={destForm.name}
                    onChange={(e) => setDestForm({ ...destForm, name: e.target.value })}
                    required
                  />
                </div>

                <div>
                  <label className="form-label">存储协议与类型 *</label>
                  <select
                    className="select"
                    value={destForm.type}
                    onChange={(e) => setDestForm({ ...destForm, type: e.target.value as DestinationType })}
                    disabled={!!editingDest}
                  >
                    <option value="LOCAL">LOCAL (服务器本地目录 / 挂载卷)</option>
                    <option value="S3">S3 (兼容 S3 协议：AWS / MinIO / Cloudflare R2 / 阿里云OSS)</option>
                    <option value="SFTP">SFTP (基于 SSH 的远程安全传输)</option>
                    <option value="SMB">SMB (Windows / 群晖 NAS 局域网文件共享)</option>
                  </select>
                </div>

                {/* LOCAL 配置 */}
                {destForm.type === 'LOCAL' && (
                  <div>
                    <label className="form-label">本地绝对路径 *</label>
                    <input
                      type="text"
                      className="input"
                      value={destForm.localPath}
                      onChange={(e) => setDestForm({ ...destForm, localPath: e.target.value })}
                      placeholder="/data/backups"
                      required
                    />
                    <small className="muted">服务进程必须对该目录拥有读写权限；系统具备路径穿越防御。</small>
                  </div>
                )}

                {/* S3 配置 */}
                {destForm.type === 'S3' && (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.8rem' }}>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.8rem' }}>
                      <div>
                        <label className="form-label">Endpoint (端点) *</label>
                        <input
                          type="text"
                          className="input"
                          placeholder="s3.amazonaws.com 或 minio:9000"
                          value={destForm.s3Endpoint}
                          onChange={(e) => setDestForm({ ...destForm, s3Endpoint: e.target.value })}
                          required
                        />
                      </div>
                      <div>
                        <label className="form-label">Region (区域)</label>
                        <input
                          type="text"
                          className="input"
                          placeholder="us-east-1 / auto"
                          value={destForm.s3Region}
                          onChange={(e) => setDestForm({ ...destForm, s3Region: e.target.value })}
                        />
                      </div>
                    </div>

                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.8rem' }}>
                      <div>
                        <label className="form-label">Bucket (存储桶名) *</label>
                        <input
                          type="text"
                          className="input"
                          placeholder="my-aie-backups"
                          value={destForm.s3Bucket}
                          onChange={(e) => setDestForm({ ...destForm, s3Bucket: e.target.value })}
                          required
                        />
                      </div>
                      <div>
                        <label className="form-label">对象路径前缀 (Prefix)</label>
                        <input
                          type="text"
                          className="input"
                          placeholder="aie-backup"
                          value={destForm.s3Prefix}
                          onChange={(e) => setDestForm({ ...destForm, s3Prefix: e.target.value })}
                        />
                      </div>
                    </div>

                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.8rem' }}>
                      <div>
                        <label className="form-label">Access Key ID *</label>
                        <input
                          type="text"
                          className="input"
                          value={destForm.s3AccessKey}
                          onChange={(e) => setDestForm({ ...destForm, s3AccessKey: e.target.value })}
                          required
                        />
                      </div>
                      <div>
                        <label className="form-label">
                          Secret Access Key {editingDest ? '(留空不修改)' : '*'}
                        </label>
                        <input
                          type="password"
                          className="input"
                          value={destForm.s3SecretKey}
                          onChange={(e) => setDestForm({ ...destForm, s3SecretKey: e.target.value })}
                          required={!editingDest}
                        />
                      </div>
                    </div>

                    <div style={{ display: 'flex', gap: '1.5rem', marginTop: '0.4rem' }}>
                      <label style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.85rem' }}>
                        <input
                          type="checkbox"
                          checked={destForm.s3UseSSL}
                          onChange={(e) => setDestForm({ ...destForm, s3UseSSL: e.target.checked })}
                        />
                        启用 HTTPS (SSL)
                      </label>
                      <label style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.85rem' }}>
                        <input
                          type="checkbox"
                          checked={destForm.s3PathStyle}
                          onChange={(e) => setDestForm({ ...destForm, s3PathStyle: e.target.checked })}
                        />
                        Path-Style 路径寻址 (适用于 MinIO)
                      </label>
                    </div>
                  </div>
                )}

                {/* SFTP 配置 */}
                {destForm.type === 'SFTP' && (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.8rem' }}>
                    <div style={{ display: 'grid', gridTemplateColumns: '2fr 1fr', gap: '0.8rem' }}>
                      <div>
                        <label className="form-label">SSH 主机地址 (Host) *</label>
                        <input
                          type="text"
                          className="input"
                          placeholder="backup.example.com"
                          value={destForm.sftpHost}
                          onChange={(e) => setDestForm({ ...destForm, sftpHost: e.target.value })}
                          required
                        />
                      </div>
                      <div>
                        <label className="form-label">端口 (Port)</label>
                        <input
                          type="number"
                          className="input"
                          value={destForm.sftpPort}
                          onChange={(e) => setDestForm({ ...destForm, sftpPort: Number(e.target.value) })}
                        />
                      </div>
                    </div>

                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.8rem' }}>
                      <div>
                        <label className="form-label">用户名 (Username) *</label>
                        <input
                          type="text"
                          className="input"
                          value={destForm.sftpUser}
                          onChange={(e) => setDestForm({ ...destForm, sftpUser: e.target.value })}
                          required
                        />
                      </div>
                      <div>
                        <label className="form-label">登录密码</label>
                        <input
                          type="password"
                          className="input"
                          placeholder={editingDest ? '留空不修改' : ''}
                          value={destForm.sftpPass}
                          onChange={(e) => setDestForm({ ...destForm, sftpPass: e.target.value })}
                        />
                      </div>
                    </div>

                    <div>
                      <label className="form-label">SSH 私钥 (PEM 格式，可选)</label>
                      <textarea
                        className="input"
                        rows={3}
                        placeholder="-----BEGIN RSA PRIVATE KEY-----"
                        value={destForm.sftpKey}
                        onChange={(e) => setDestForm({ ...destForm, sftpKey: e.target.value })}
                      />
                    </div>

                    <div>
                      <label className="form-label">远端保存路径 (Remote Path) *</label>
                      <input
                        type="text"
                        className="input"
                        placeholder="/backup/ai-employee"
                        value={destForm.sftpPath}
                        onChange={(e) => setDestForm({ ...destForm, sftpPath: e.target.value })}
                        required
                      />
                    </div>
                  </div>
                )}

                {/* SMB 配置 */}
                {destForm.type === 'SMB' && (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.8rem' }}>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.8rem' }}>
                      <div>
                        <label className="form-label">SMB 服务器 (Server) *</label>
                        <input
                          type="text"
                          className="input"
                          placeholder="192.168.1.100"
                          value={destForm.smbServer}
                          onChange={(e) => setDestForm({ ...destForm, smbServer: e.target.value })}
                          required
                        />
                      </div>
                      <div>
                        <label className="form-label">共享文件夹 (Share) *</label>
                        <input
                          type="text"
                          className="input"
                          placeholder="Backup"
                          value={destForm.smbShare}
                          onChange={(e) => setDestForm({ ...destForm, smbShare: e.target.value })}
                          required
                        />
                      </div>
                    </div>

                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.8rem' }}>
                      <div>
                        <label className="form-label">用户名 (Username) *</label>
                        <input
                          type="text"
                          className="input"
                          value={destForm.smbUser}
                          onChange={(e) => setDestForm({ ...destForm, smbUser: e.target.value })}
                          required
                        />
                      </div>
                      <div>
                        <label className="form-label">密码 (Password)</label>
                        <input
                          type="password"
                          className="input"
                          placeholder={editingDest ? '留空不修改' : ''}
                          value={destForm.smbPass}
                          onChange={(e) => setDestForm({ ...destForm, smbPass: e.target.value })}
                        />
                      </div>
                    </div>

                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.8rem' }}>
                      <div>
                        <label className="form-label">域名 (Domain，可选)</label>
                        <input
                          type="text"
                          className="input"
                          value={destForm.smbDomain}
                          onChange={(e) => setDestForm({ ...destForm, smbDomain: e.target.value })}
                        />
                      </div>
                      <div>
                        <label className="form-label">子目录路径 (可选)</label>
                        <input
                          type="text"
                          className="input"
                          placeholder="aie-backups"
                          value={destForm.smbPath}
                          onChange={(e) => setDestForm({ ...destForm, smbPath: e.target.value })}
                        />
                      </div>
                    </div>
                  </div>
                )}
              </div>

              <div className="modal-footer">
                <button type="button" className="btn btn-secondary" onClick={() => setDestModalOpen(false)}>
                  取消
                </button>
                <button type="submit" className="btn btn-primary">
                  {editingDest ? '保存修改' : '创建存储目标'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* 弹窗 2: 创建/编辑备份策略 (含加密开关选项) */}
      {policyModalOpen && (
        <div className="modal-backdrop">
          <div className="modal-card" style={{ maxWidth: '640px' }}>
            <div className="modal-header">
              <h3>{editingPolicy ? '编辑备份策略' : '新建备份策略'}</h3>
              <button type="button" className="modal-close-btn" onClick={() => setPolicyModalOpen(false)}>
                ✕
              </button>
            </div>
            <form onSubmit={handlePolicySubmit}>
              <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
                <div>
                  <label className="form-label">策略名称 *</label>
                  <input
                    type="text"
                    className="input"
                    placeholder="例如：每日全量核心备份 或 每周离线归档"
                    value={policyForm.name}
                    onChange={(e) => setPolicyForm({ ...policyForm, name: e.target.value })}
                    required
                  />
                </div>

                {/* 调度设置 */}
                <div>
                  <label className="form-label">执行计划 (Schedule)</label>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.8rem' }}>
                    <select
                      className="select"
                      value={policyForm.cronPreset}
                      onChange={(e) => {
                        const val = e.target.value
                        let expr = policyForm.cronExpression
                        if (val === 'daily_02') expr = '0 2 * * *'
                        else if (val === 'daily_04') expr = '0 4 * * *'
                        else if (val === 'weekly_sun') expr = '0 3 * * 0'
                        setPolicyForm({ ...policyForm, cronPreset: val, cronExpression: expr })
                      }}
                    >
                      <option value="daily_02">每天凌晨 02:00</option>
                      <option value="daily_04">每天凌晨 04:00</option>
                      <option value="weekly_sun">每周日凌晨 03:00</option>
                      <option value="custom">自定义 Cron 表达式</option>
                    </select>
                    <input
                      type="text"
                      className="input"
                      style={{ fontFamily: 'var(--font-mono)' }}
                      placeholder="Cron (分 时 日 月 周)"
                      value={policyForm.cronExpression}
                      onChange={(e) => setPolicyForm({ ...policyForm, cronExpression: e.target.value, cronPreset: 'custom' })}
                      required
                    />
                  </div>
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.8rem' }}>
                  <div>
                    <label className="form-label">时区 (Timezone)</label>
                    <input
                      type="text"
                      className="input"
                      value={policyForm.timezone}
                      onChange={(e) => setPolicyForm({ ...policyForm, timezone: e.target.value })}
                      required
                    />
                  </div>
                  <div>
                    <label className="form-label">保留成功备份数 (Keep Last N)</label>
                    <input
                      type="number"
                      className="input"
                      min={1}
                      max={100}
                      value={policyForm.retentionKeepLast}
                      onChange={(e) => setPolicyForm({ ...policyForm, retentionKeepLast: Number(e.target.value) })}
                      required
                    />
                  </div>
                </div>

                {/* 存储目标多选 */}
                <div>
                  <label className="form-label">分发存储目标 (至少选择一个) *</label>
                  <div
                    style={{
                      display: 'flex',
                      flexDirection: 'column',
                      gap: '0.5rem',
                      maxHeight: '140px',
                      overflowY: 'auto',
                      border: '1px solid var(--border-subtle)',
                      borderRadius: 'var(--radius-sm)',
                      padding: '0.6rem',
                    }}
                  >
                    {destinations.map((d) => (
                      <label key={d.id} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem' }}>
                        <input
                          type="checkbox"
                          checked={policyForm.destinationIDs.includes(d.id)}
                          onChange={(e) => {
                            if (e.target.checked) {
                              setPolicyForm({
                                ...policyForm,
                                destinationIDs: [...policyForm.destinationIDs, d.id],
                              })
                            } else {
                              setPolicyForm({
                                ...policyForm,
                                destinationIDs: policyForm.destinationIDs.filter((id) => id !== d.id),
                              })
                            }
                          }}
                        />
                        <span className="badge" style={{ fontSize: '0.75rem' }}>{d.type}</span>
                        <strong>{d.name}</strong>
                        {!d.enabled && <span style={{ color: 'var(--danger)', fontSize: '0.75rem' }}>(已停用)</span>}
                      </label>
                    ))}
                    {destinations.length === 0 && (
                      <span className="muted">暂无可用的存储目标，请先在存储目标 Tab 中添加</span>
                    )}
                  </div>
                </div>

                {/* 加密与压缩设置（明确响应用户对可设置不加密的要求） */}
                <div
                  style={{
                    background: 'var(--bg-main)',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 'var(--radius-md)',
                    padding: '0.9rem',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: '0.7rem',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                    <div>
                      <strong style={{ fontSize: '0.9rem' }}>开启备份包加密 (AES-256-GCM)</strong>
                      <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)', marginTop: '2px' }}>
                        根据备份设置决定是否加密。关闭时生成明文压缩包，还原无需解密密钥。
                      </div>
                    </div>
                    <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer' }}>
                      <input
                        type="checkbox"
                        checked={policyForm.encryptionEnabled}
                        onChange={(e) => setPolicyForm({ ...policyForm, encryptionEnabled: e.target.checked })}
                        style={{ width: '18px', height: '18px', accentColor: 'var(--brand-500)' }}
                      />
                    </label>
                  </div>

                  {policyForm.encryptionEnabled ? (
                    <div style={{ fontSize: '0.78rem', color: 'var(--brand-700)', display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                      <IconKey size={14} />
                      加密模式已启用：生成的快照包将使用 Center Server Master Key 进行流式强加密保护。
                    </div>
                  ) : (
                    <div style={{ fontSize: '0.78rem', color: '#b45309', display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                      <IconPackage size={14} />
                      不加密模式：备份包以标准 Zstandard 格式保存，方便运维外部归档检查或迁移。
                    </div>
                  )}
                </div>
              </div>

              <div className="modal-footer">
                <button type="button" className="btn btn-secondary" onClick={() => setPolicyModalOpen(false)}>
                  取消
                </button>
                <button type="submit" className="btn btn-primary">
                  {editingPolicy ? '保存策略修改' : '创建策略'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* 弹窗 3: 手动执行即时备份 (支持选择不加密) */}
      {manualModalOpen && (
        <div className="modal-backdrop">
          <div className="modal-card" style={{ maxWidth: '520px' }}>
            <div className="modal-header">
              <h3>立即发起 Center Server 全量备份</h3>
              <button type="button" className="modal-close-btn" onClick={() => setManualModalOpen(false)}>
                ✕
              </button>
            </div>
            <form onSubmit={handleManualSubmit}>
              <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
                <p style={{ margin: 0, fontSize: '0.85rem', color: 'var(--text-secondary)' }}>
                  将立即对 PostgreSQL 数据库完整事务快照、CA 证书私钥、Secret 保管箱与任务产物执行全量归档。
                </p>

                <div>
                  <label className="form-label">分发存储目标 *</label>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
                    {destinations.map((d) => (
                      <label key={d.id} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem' }}>
                        <input
                          type="checkbox"
                          checked={manualDestIDs.includes(d.id)}
                          onChange={(e) => {
                            if (e.target.checked) setManualDestIDs([...manualDestIDs, d.id])
                            else setManualDestIDs(manualDestIDs.filter((id) => id !== d.id))
                          }}
                        />
                        <span className="badge">{d.type}</span>
                        <span>{d.name}</span>
                      </label>
                    ))}
                  </div>
                </div>

                <div
                  style={{
                    background: 'var(--bg-main)',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 'var(--radius-md)',
                    padding: '0.8rem',
                  }}
                >
                  <label style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', cursor: 'pointer' }}>
                    <input
                      type="checkbox"
                      checked={manualEncEnabled}
                      onChange={(e) => setManualEncEnabled(e.target.checked)}
                      style={{ width: '18px', height: '18px', accentColor: 'var(--brand-500)' }}
                    />
                    <div>
                      <strong style={{ fontSize: '0.9rem' }}>启用备份包加密 (AES-256)</strong>
                      <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                        可取消勾选设置为不加密
                      </div>
                    </div>
                  </label>
                </div>
              </div>

              <div className="modal-footer">
                <button type="button" className="btn btn-secondary" onClick={() => setManualModalOpen(false)}>
                  取消
                </button>
                <button type="submit" className="btn btn-primary">
                  开始生成备份
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* 弹窗 4: 校验结果报告 */}
      {verifyReport && (
        <div className="modal-backdrop">
          <div className="modal-card" style={{ maxWidth: '580px' }}>
            <div className="modal-header">
              <h3 style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <IconCheckCircle size={22} style={{ color: verifyReport.passed ? 'var(--success)' : 'var(--danger)' }} />
                备份完整性校验报告
              </h3>
              <button type="button" className="modal-close-btn" onClick={() => setVerifyReport(null)}>
                ✕
              </button>
            </div>
            <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '0.8rem' }}>
              <div
                style={{
                  padding: '0.8rem',
                  borderRadius: 'var(--radius-md)',
                  background: verifyReport.passed ? 'var(--brand-50)' : 'var(--danger-bg)',
                  color: verifyReport.passed ? 'var(--brand-700)' : 'var(--danger)',
                  fontWeight: 600,
                }}
              >
                {verifyReport.passed ? '✓ 备份完整性验证通过：哈希及归档文件结构完全一致' : '✕ 校验失败：检测到异常或不匹配'}
              </div>

              <div style={{ fontSize: '0.85rem', display: 'flex', flexDirection: 'column', gap: '0.4rem' }}>
                <div><strong>备份 ID:</strong> <code>{verifyReport.backup_id}</code></div>
                <div><strong>SHA-256:</strong> <code style={{ wordBreak: 'break-all' }}>{verifyReport.checksum}</code></div>
                <div><strong>加密格式:</strong> {verifyReport.encryption}</div>
                <div><strong>文件总数:</strong> {verifyReport.file_count} 个</div>
              </div>

              <div>
                <strong style={{ fontSize: '0.85rem' }}>校验详情记录:</strong>
                <ul style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', paddingLeft: '1.2rem', margin: '0.4rem 0 0' }}>
                  {verifyReport.details.map((d, i) => (
                    <li key={i}>{d}</li>
                  ))}
                </ul>
              </div>
            </div>
            <div className="modal-footer">
              <button type="button" className="btn btn-primary" onClick={() => setVerifyReport(null)}>
                关闭报告
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 弹窗 5: 容灾恢复确认弹窗 */}
      {restoreConfirmRun && (
        <div className="modal-backdrop" onClick={() => !restoring && setRestoreConfirmRun(null)}>
          <div className="modal-card" style={{ maxWidth: '540px' }} onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3 style={{ color: 'var(--danger)', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <IconShield size={20} />
                <span>全系统容灾恢复 (Disaster Recovery)</span>
              </h3>
              <button
                type="button"
                className="modal-close-btn"
                onClick={() => setRestoreConfirmRun(null)}
                disabled={restoring}
              >
                ✕
              </button>
            </div>
            <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div
                style={{
                  padding: '0.85rem',
                  borderRadius: 'var(--radius-sm)',
                  background: 'var(--bg-main)',
                  border: '1px solid var(--border-subtle)',
                  fontSize: '0.84rem',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '0.35rem',
                }}
              >
                <div>
                  <strong>目标快照:</strong>{' '}
                  <code style={{ fontFamily: 'var(--font-mono)', fontWeight: 600 }}>{restoreConfirmRun.backup_id}</code>
                </div>
                <div>
                  <strong>创建时间:</strong> {formatTime(restoreConfirmRun.created_at)}
                </div>
                <div>
                  <strong>归档体积:</strong> {formatBytes(restoreConfirmRun.artifact_size)} ·{' '}
                  <strong>加密状态:</strong> {restoreConfirmRun.manifest?.encryption?.algorithm === 'NONE' ? '未加密 (明文)' : 'AES-256-GCM 密文'}
                </div>
              </div>

              <div
                style={{
                  padding: '0.85rem',
                  borderRadius: 'var(--radius-md)',
                  background: 'var(--brand-50)',
                  border: '1px solid var(--brand-100)',
                  fontSize: '0.82rem',
                  color: 'var(--brand-700)',
                  lineHeight: 1.5,
                }}
              >
                <strong>🛡️ 零丢损安全承诺：</strong>
                <br />
                在覆盖当前运行数据前，系统将<strong>自动打一份当前状态的 Emergency Backup 应急快照</strong>。若还原过程异常中断，系统将完好保护现状，支持快速倒退。
              </div>

              {restoreModalError && (
                <div
                  style={{
                    padding: '0.75rem',
                    borderRadius: 'var(--radius-sm)',
                    background: '#fef2f2',
                    border: '1px solid #fecaca',
                    color: '#b91c1c',
                    fontSize: '0.82rem',
                  }}
                >
                  {restoreModalError}
                </div>
              )}

              <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                <div className="form-group" style={{ margin: 0 }}>
                  <label style={{ fontSize: '0.84rem', fontWeight: 600 }}>
                    管理员二次核验密码 (Step-Up)
                  </label>
                  <input
                    type="password"
                    className="form-control"
                    placeholder="请输入当前管理员登录密码完成提权"
                    value={restoreAdminPassword}
                    onChange={(e) => setRestoreAdminPassword(e.target.value)}
                    disabled={restoring}
                    autoFocus
                  />
                  <small style={{ color: 'var(--text-muted)', fontSize: '0.74rem' }}>
                    容灾还原属于破坏性全量覆盖操作，需要管理员密码短时身份核验
                  </small>
                </div>

                <div className="form-group" style={{ margin: 0 }}>
                  <label style={{ fontSize: '0.84rem', fontWeight: 600 }}>
                    TOTP 动态口令 (若已绑定双因子认证)
                  </label>
                  <input
                    type="text"
                    className="form-control"
                    placeholder="6 位数字动态验证码（未开启可留空）"
                    value={restoreTotpCode}
                    onChange={(e) => setRestoreTotpCode(e.target.value)}
                    disabled={restoring}
                    maxLength={8}
                  />
                </div>
              </div>
            </div>

            <div className="modal-footer">
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setRestoreConfirmRun(null)}
                disabled={restoring}
              >
                取消
              </button>
              <button
                type="button"
                className="btn btn-danger"
                onClick={() => void handleRestoreSubmit()}
                disabled={restoring}
              >
                {restoring ? '正在拉起恢复...' : '确认执行恢复'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 抽屉: 备份详情 */}
      {detailRun && (
        <div className="modal-backdrop" onClick={() => setDetailRun(null)}>
          <div
            className="modal-card"
            style={{ maxWidth: '680px', maxHeight: '85vh', overflowY: 'auto' }}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="modal-header">
              <h3>备份快照详情</h3>
              <button type="button" className="modal-close-btn" onClick={() => setDetailRun(null)}>
                ✕
              </button>
            </div>
            <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.8rem', fontSize: '0.85rem' }}>
                <div><strong>Backup ID:</strong> <code>{detailRun.backup_id}</code></div>
                <div><strong>生成时间:</strong> {formatTime(detailRun.created_at)}</div>
                <div><strong>耗时:</strong> {formatDuration(detailRun.duration_ms)}</div>
                <div><strong>归档尺寸:</strong> {formatBytes(detailRun.artifact_size)}</div>
                <div><strong>文件总数:</strong> {detailRun.file_count}</div>
                <div><strong>触发来源:</strong> {detailRun.trigger_type}</div>
              </div>

              <div>
                <strong style={{ fontSize: '0.85rem' }}>SHA-256 Checksum:</strong>
                <div
                  style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: '0.8rem',
                    background: 'var(--bg-main)',
                    padding: '0.5rem',
                    borderRadius: 'var(--radius-sm)',
                    wordBreak: 'break-all',
                    marginTop: '0.3rem',
                  }}
                >
                  {detailRun.checksum || '—'}
                </div>
              </div>

              <div>
                <strong style={{ fontSize: '0.85rem' }}>多目标分发明细:</strong>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.4rem', marginTop: '0.4rem' }}>
                  {detailRun.destinations?.map((rd) => (
                    <div
                      key={rd.id}
                      style={{
                        padding: '0.5rem 0.75rem',
                        border: '1px solid var(--border-subtle)',
                        borderRadius: 'var(--radius-sm)',
                        fontSize: '0.8rem',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                      }}
                    >
                      <div>
                        <strong>{rd.destination_name || rd.destination_id}</strong> ({rd.destination_type})
                        <div style={{ color: 'var(--text-muted)', fontSize: '0.72rem' }}>{rd.remote_path}</div>
                      </div>
                      <StatusBadge status={rd.status} />
                    </div>
                  ))}
                </div>
              </div>

              <div>
                <strong style={{ fontSize: '0.85rem' }}>Manifest 元数据:</strong>
                <pre
                  style={{
                    fontFamily: 'var(--font-mono)',
                    fontSize: '0.75rem',
                    background: 'var(--bg-main)',
                    padding: '0.75rem',
                    borderRadius: 'var(--radius-sm)',
                    overflowX: 'auto',
                    maxHeight: '220px',
                  }}
                >
                  {JSON.stringify(detailRun.manifest, null, 2)}
                </pre>
              </div>
            </div>
            <div className="modal-footer">
              <button type="button" className="btn btn-primary" onClick={() => setDetailRun(null)}>
                关闭
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 通用删除确认弹窗 */}
      <ConfirmDialog
        open={!!deleteTarget}
        title={deleteTarget?.title || '确认删除'}
        description={deleteTarget ? `确认删除「${deleteTarget.label}」？此操作不可逆。` : ''}
        targetLabel={deleteTarget?.label}
        targetMeta={deleteTarget?.meta}
        confirmText="确认删除"
        danger
        busy={deleteBusy}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => void handleConfirmDelete()}
      />
    </div>
  )
}

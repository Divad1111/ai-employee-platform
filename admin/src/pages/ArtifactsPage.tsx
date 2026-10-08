import { useEffect, useState } from 'react'
import { apiGet, getToken } from '../api/client'
import { IconPackage, IconRefresh } from '../components/Icons'
import { EntityName } from '../components/EntityName'
import { MarkdownView } from '../components/MarkdownView'
import { PageFeatureGuide } from '../components/PageFeatureGuide'
import { formatDateTime } from '../lib/time'

type Artifact = {
  id: string
  job_id: string
  name: string
  type: string
  size_bytes: number
  sha256: string
  created_at: string
}

type JobItem = {
  id: string
  prompt: string
}

function formatBytes(bytes: number) {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`
}

export function ArtifactsPage() {
  const [items, setItems] = useState<Artifact[]>([])
  const [jobMap, setJobMap] = useState<Record<string, string>>({})
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)
  const [preview, setPreview] = useState<{ name: string; text: string } | null>(null)

  const reload = () => {
    setLoading(true)
    Promise.all([
      apiGet<{ items: Artifact[] }>('/artifacts').catch((e: Error) => {
        setErr(e.message)
        return { items: [] as Artifact[] }
      }),
      apiGet<{ items: JobItem[] }>('/jobs').catch(() => ({ items: [] as JobItem[] })),
    ])
      .then(([artData, jobsData]) => {
        setItems(artData.items ?? [])
        setJobMap(Object.fromEntries((jobsData.items ?? []).map((j) => [j.id, j.prompt])))
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    reload()
  }, [])

  const isText = (a: Artifact) => {
    const name = a.name.toLowerCase()
    const typ = (a.type || '').toLowerCase()
    return typ === 'md' || typ === 'markdown' || typ === 'txt' || typ === 'log' || typ === 'result' || typ === 'report'
      || name.endsWith('.md') || name.endsWith('.markdown') || name.endsWith('.txt')
  }

  const loadText = async (id: string) => {
    const tok = getToken()
    const res = await fetch(`/api/artifacts/${id}/download`, {
      headers: tok ? { Authorization: `Bearer ${tok}` } : {},
    })
    if (!res.ok) throw new Error(`读取失败 (状态码: ${res.status})`)
    return res.text()
  }

  const openPreview = (a: Artifact) => {
    setErr('')
    void loadText(a.id)
      .then((text) => setPreview({ name: a.name, text }))
      .catch((e: Error) => setErr(e.message))
  }

  const download = (id: string, name: string) => {
    const tok = getToken()
    void fetch(`/api/artifacts/${id}/download`, {
      headers: tok ? { Authorization: `Bearer ${tok}` } : {},
    })
      .then(async (res) => {
        if (!res.ok) throw new Error(`下载失败 (状态码: ${res.status})`)
        const blob = await res.blob()
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = name
        a.click()
        URL.revokeObjectURL(url)
      })
      .catch((e: Error) => setErr(e.message))
  }

  return (
    <section>
      <header className="page-header">
        <div>
          <h1>任务制品产物中心 (Artifacts)</h1>
          <p>任务执行产出的代码包、日志报表与生成物 · 文本默认按 Markdown 展示</p>
        </div>
        <button type="button" className="btn-ghost" onClick={() => reload()} disabled={loading}>
          <IconRefresh size={15} />
          <span>刷新制品</span>
        </button>
      </header>

      <PageFeatureGuide
        title="任务制品产物保全与版本防篡改指引"
        summary="集中归档 AI Agent 在任务执行过程中导出的二进制构建包、Diff 补丁文件、架构设计图及全流程输出报告。"
        steps={[
          {
            step: '1',
            title: 'SHA-256 指纹校验与去重',
            desc: '所有上传制品自动计算哈希指纹，防止重复冗余落盘并确保证据链不可篡改。',
            tag: '完整性存证',
          },
          {
            step: '2',
            title: '任务全链路追溯绑定',
            desc: '每个制品强制关联派生它的 Job ID，可一键跳转回溯当时的上下文 Prompt 与执行输出。',
            tag: '来源追溯',
          },
          {
            step: '3',
            title: '安全流式直接下载',
            desc: '内置 JWT Bearer 鉴权保护，管理员可随时一键下载产物包到本地进行回归测试与部署。',
            tag: '交付归档',
          },
        ]}
      />

      {err ? <div className="error">{err}</div> : null}

      <div className="panel">
        <div className="panel-header">
          <div>
            <h2>产物制品清单</h2>
            <p>共归档 {items.length} 份任务输出产物</p>
          </div>
        </div>

        <div className="table-wrapper">
          <table className="table">
            <thead>
              <tr>
                <th>产物文件名</th>
                <th>关联任务需求</th>
                <th>SHA-256 内容校验哈希</th>
                <th>文件大小</th>
                <th>归档时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((a) => (
                <tr key={a.id}>
                  <td>
                    <EntityName
                      name={a.name}
                      id={a.id}
                      icon={<IconPackage size={16} style={{ color: 'var(--brand-600)' }} />}
                    />
                  </td>
                  <td style={{ maxWidth: '240px' }}>
                    <EntityName
                      name={jobMap[a.job_id] || `任务 #${a.job_id.slice(0, 8)}`}
                      id={a.job_id}
                      to={`/jobs/${a.job_id}`}
                    />
                  </td>
                  <td>
                    <span className="mono" title={a.sha256}>
                      {a.sha256 ? `${a.sha256.slice(0, 16)}…` : '—'}
                    </span>
                  </td>
                  <td>{formatBytes(a.size_bytes)}</td>
                  <td style={{ fontSize: '0.82rem', color: 'var(--text-muted)' }}>
                    {a.created_at ? formatDateTime(a.created_at) : '—'}
                  </td>
                  <td>
                    <div style={{ display: 'flex', gap: '0.35rem', flexWrap: 'wrap' }}>
                    {isText(a) ? (
                      <button type="button" className="btn-ghost btn-sm" onClick={() => openPreview(a)}>
                        查看 Markdown
                      </button>
                    ) : null}
                    <button type="button" className="btn-ghost btn-sm" onClick={() => download(a.id, a.name)}>
                      下载制品文件
                    </button>
                    </div>
                  </td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={6} className="empty-tip">暂无归档制品产物</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      {preview ? (
        <div className="panel">
          <div className="panel-header">
            <div>
              <h2>文本预览</h2>
              <p>{preview.name} · 默认按 Markdown 渲染</p>
            </div>
            <button type="button" className="btn-ghost" onClick={() => setPreview(null)}>
              关闭预览
            </button>
          </div>
          <MarkdownView text={preview.text} />
        </div>
      ) : null}
    </section>
  )
}

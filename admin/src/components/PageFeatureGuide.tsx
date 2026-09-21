/**
 * 页面功能引导卡片组件（对齐飞书协同集成页面的指引风格）
 */
import { useState } from 'react'

export type StepGuide = {
  step: string
  title: string
  desc: string
  tag?: string
}

export type PageFeatureGuideProps = {
  badge?: string
  title: string
  summary: string
  steps: StepGuide[]
  actionText?: string
  actionHref?: string
  actionTo?: string
  onAction?: () => void
}

export function PageFeatureGuide({
  badge = '功能说明与业务架构指引',
  title,
  summary,
  steps,
  actionText,
  actionHref,
  onAction,
}: PageFeatureGuideProps) {
  const [collapsed, setCollapsed] = useState(true)

  return (
    <div
      className="panel"
      style={{
        borderLeft: '4px solid var(--brand-500)',
        background: '#f8fafc',
        marginBottom: '1.25rem',
        padding: '1rem 1.25rem',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-start',
          justifyContent: 'space-between',
          gap: '1rem',
          flexWrap: 'wrap',
          cursor: 'pointer',
        }}
        onClick={() => setCollapsed(!collapsed)}
      >
        <div style={{ flex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem', flexWrap: 'wrap' }}>
            <span
              style={{
                fontSize: '0.72rem',
                padding: '0.15rem 0.5rem',
                borderRadius: '4px',
                background: 'var(--brand-50, #eff6ff)',
                color: 'var(--brand-700, #1d4ed8)',
                fontWeight: 600,
                border: '1px solid var(--brand-200, #bfdbfe)',
              }}
            >
              {badge}
            </span>
            <h3
              style={{
                margin: 0,
                fontSize: '0.98rem',
                fontWeight: 700,
                color: 'var(--brand-800, #1e40af)',
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.4rem',
              }}
            >
              <span>📖</span>
              <span>{title}</span>
            </h3>
          </div>
          <p
            style={{
              margin: '0.35rem 0 0',
              fontSize: '0.84rem',
              color: 'var(--text-secondary)',
              lineHeight: 1.5,
            }}
          >
            {summary}
          </p>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }} onClick={(e) => e.stopPropagation()}>
          {actionText && actionHref ? (
            <a
              href={actionHref}
              target="_blank"
              rel="noreferrer"
              className="btn-ghost btn-sm"
              style={{ textDecoration: 'none', display: 'inline-flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.8rem' }}
            >
              <span>{actionText} ↗</span>
            </a>
          ) : null}
          {actionText && onAction ? (
            <button
              type="button"
              className="btn-ghost btn-sm"
              onClick={onAction}
              style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', fontSize: '0.8rem' }}
            >
              <span>{actionText}</span>
            </button>
          ) : null}
          <button
            type="button"
            className="btn-ghost btn-sm"
            onClick={() => setCollapsed(!collapsed)}
            style={{ fontSize: '0.75rem', padding: '0.2rem 0.5rem' }}
          >
            {collapsed ? '展开指引 ▼' : '收起指引 ▲'}
          </button>
        </div>
      </div>

      {!collapsed && (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))',
            gap: '0.75rem',
            fontSize: '0.83rem',
            marginTop: '0.85rem',
          }}
        >
          {steps.map((s, idx) => (
            <div
              key={idx}
              style={{
                background: '#ffffff',
                padding: '0.75rem 0.95rem',
                borderRadius: '6px',
                border: '1px solid #e2e8f0',
                display: 'flex',
                flexDirection: 'column',
                gap: '0.3rem',
              }}
            >
              <div
                style={{
                  fontWeight: 700,
                  color: 'var(--brand-700, #1d4ed8)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: '0.5rem',
                }}
              >
                <span>{s.step}. {s.title}</span>
                {s.tag ? (
                  <span
                    style={{
                      fontSize: '0.7rem',
                      fontWeight: 500,
                      padding: '0.1rem 0.4rem',
                      borderRadius: '3px',
                      background: '#f1f5f9',
                      color: '#475569',
                    }}
                  >
                    {s.tag}
                  </span>
                ) : null}
              </div>
              <p
                style={{
                  margin: 0,
                  color: 'var(--text-secondary)',
                  lineHeight: 1.5,
                  fontSize: '0.81rem',
                }}
              >
                {s.desc}
              </p>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

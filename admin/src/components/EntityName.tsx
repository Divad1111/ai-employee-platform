/**
 * EntityName: 优先显示人类友好名称，提供一键展开查看与复制完整 ID。
 */
import React, { useState } from 'react'
import { Link } from 'react-router-dom'

interface EntityNameProps {
  name?: string
  id?: string
  to?: string
  sub?: string
  icon?: React.ReactNode
  fallback?: string
  maxLength?: number
}

export function EntityName({
  name,
  id,
  to,
  sub,
  icon,
  fallback = '—',
  maxLength,
}: EntityNameProps) {
  const [showId, setShowId] = useState(false)
  const [copied, setCopied] = useState(false)

  // 友好名称优先
  let primaryName = name && name.trim() ? name.trim() : (id || fallback)
  if (maxLength && primaryName.length > maxLength) {
    primaryName = primaryName.slice(0, maxLength) + '…'
  }

  // 是否存在需要展示的独立 ID
  const hasId = Boolean(id && id !== primaryName)

  function onCopy(e: React.MouseEvent) {
    e.stopPropagation()
    e.preventDefault()
    if (id) {
      void navigator.clipboard.writeText(id)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  const nameElem = to ? (
    <Link
      to={to}
      style={{
        fontWeight: 600,
        color: 'var(--brand-700)',
        textDecoration: 'none',
      }}
    >
      {primaryName}
    </Link>
  ) : (
    <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{primaryName}</span>
  )

  return (
    <div style={{ display: 'inline-flex', flexDirection: 'column', gap: '0.15rem', verticalAlign: 'middle', maxWidth: '100%' }}>
      <div style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem', flexWrap: 'wrap' }}>
        {icon}
        {nameElem}
        {hasId && (
          <button
            type="button"
            className="btn-ghost"
            style={{
              padding: '0.05rem 0.35rem',
              fontSize: '0.68rem',
              borderRadius: '4px',
              background: '#f1f5f9',
              border: '1px solid #e2e8f0',
              color: '#64748b',
              lineHeight: 1.2,
              cursor: 'pointer',
              display: 'inline-flex',
              alignItems: 'center',
              gap: '0.2rem',
            }}
            title={showId ? '点击隐藏 ID' : `点击查看完整 ID: ${id}`}
            onClick={(e) => {
              e.stopPropagation()
              e.preventDefault()
              setShowId((prev) => !prev)
            }}
          >
            <span>{showId ? '收起 ID' : 'ID'}</span>
          </button>
        )}
      </div>

      {showId && hasId && (
        <div
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: '0.4rem',
            background: '#f8fafc',
            border: '1px dashed #cbd5e1',
            borderRadius: '4px',
            padding: '0.15rem 0.4rem',
            fontSize: '0.72rem',
            fontFamily: 'monospace',
            color: '#475569',
            marginTop: '0.1rem',
            width: 'fit-content',
          }}
        >
          <span>{id}</span>
          <button
            type="button"
            onClick={onCopy}
            style={{
              background: 'none',
              border: 'none',
              cursor: 'pointer',
              fontSize: '0.72rem',
              color: 'var(--brand-600)',
              padding: 0,
              fontWeight: 600,
            }}
            title="复制完整 ID"
          >
            {copied ? '✅ 已复制' : '📋 复制'}
          </button>
        </div>
      )}

      {sub && !showId && (
        <div style={{ fontSize: '0.72rem', color: 'var(--text-muted)' }}>{sub}</div>
      )}
    </div>
  )
}

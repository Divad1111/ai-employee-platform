/**
 * 可输入筛选的下拉：点开先列出全部；输入后按中文 label / value 过滤。
 */
import { useEffect, useId, useMemo, useRef, useState, type CSSProperties } from 'react'

export type SearchOption = {
  value: string
  label: string
  /** 额外参与匹配的关键词 */
  keywords?: string
}

type Props = {
  value: string
  onChange: (value: string) => void
  options: SearchOption[]
  placeholder?: string
  /** 允许输入不在列表中的值（如 UUID） */
  allowCustom?: boolean
  required?: boolean
  disabled?: boolean
  id?: string
  className?: string
  style?: CSSProperties
  /** 下拉展开时回调（例如向工作站拉取最新选项） */
  onOpen?: () => void
}

function matchOption(opt: SearchOption, q: string) {
  const needle = q.trim().toLowerCase()
  if (!needle) return true
  const hay = `${opt.label} ${opt.value} ${opt.keywords || ''}`.toLowerCase()
  return hay.includes(needle)
}

export function SearchableSelect({
  value,
  onChange,
  options,
  placeholder = '输入筛选或选择…',
  allowCustom = false,
  required,
  disabled,
  id,
  className,
  style,
  onOpen,
}: Props) {
  const listId = useId()
  const wrapRef = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  /** 打开后是否已开始手动输入；未输入前展示全部选项 */
  const [typing, setTyping] = useState(false)

  const selected = options.find((o) => o.value === value)
  const display = open ? (typing ? query : selected?.label || value || '') : selected?.label || value

  const filtered = useMemo(() => {
    if (!open) return []
    if (!typing || !query.trim()) return options.slice(0, 80)
    return options.filter((o) => matchOption(o, query)).slice(0, 80)
  }, [open, options, query, typing])

  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) {
        setOpen(false)
        setQuery('')
        setTyping(false)
      }
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [])

  const pick = (opt: SearchOption) => {
    onChange(opt.value)
    setQuery('')
    setTyping(false)
    setOpen(false)
  }

  const commitCustom = () => {
    if (!allowCustom) return
    const v = query.trim()
    if (v) {
      onChange(v)
      setOpen(false)
      setTyping(false)
    }
  }

  return (
    <div className={`search-select${className ? ` ${className}` : ''}`} ref={wrapRef} style={style}>
      <input
        id={id}
        type="text"
        role="combobox"
        aria-expanded={open}
        aria-controls={listId}
        aria-autocomplete="list"
        disabled={disabled}
        required={required && !value}
        placeholder={placeholder}
        value={display}
        autoComplete="off"
        onFocus={() => {
          if (!open) onOpen?.()
          setOpen(true)
          setTyping(false)
          setQuery('')
        }}
        onChange={(e) => {
          const next = e.target.value
          setQuery(next)
          setTyping(true)
          setOpen(true)
          if (allowCustom) onChange(next)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            if (filtered.length === 1) pick(filtered[0])
            else commitCustom()
          } else if (e.key === 'Escape') {
            setOpen(false)
            setQuery('')
            setTyping(false)
          }
        }}
      />
      {open && !disabled ? (
        <ul id={listId} className="search-select-menu" role="listbox">
          {filtered.length === 0 ? (
            <li className="search-select-empty muted">
              {allowCustom && query.trim()
                ? `无匹配项，回车使用「${query.trim()}」`
                : '无匹配项'}
            </li>
          ) : (
            filtered.map((opt) => (
              <li key={opt.value}>
                <button
                  type="button"
                  role="option"
                  aria-selected={opt.value === value}
                  className={opt.value === value ? 'is-active' : undefined}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => pick(opt)}
                >
                  <span className="search-select-label">{opt.label}</span>
                  {opt.label !== opt.value ? (
                    <span className="search-select-code muted">{opt.value}</span>
                  ) : null}
                </button>
              </li>
            ))
          )}
        </ul>
      ) : null}
    </div>
  )
}

import { useState } from 'react'

export function CollectorListField({ label, value, placeholder = '', onChange }: {
  label: string; value: string[]; placeholder?: string; onChange: (values: string[]) => void
}) {
  const [draft, setDraft] = useState(value.join(', '))
  return <label className="col-field">{label}<input value={draft} placeholder={placeholder} onChange={e => {
    setDraft(e.target.value)
    onChange(e.target.value.split(/[,，]/).map(v => v.trim()).filter(Boolean))
  }} /></label>
}

import type { SkillAsset } from '../../api/workflowmcp'

export const MAX_EDIT_BYTES = 1 << 20
export const MAX_ASSET_BYTES = 16 << 20

export type AssetDraft = {
  path: string
  base?: SkillAsset
  baseBytes?: Uint8Array
  blob?: Blob
  text?: string
  bom?: boolean
  eol?: '\n' | '\r\n'
  changed: boolean
  deleted: boolean
}

export function decodeAsset(bytes: Uint8Array): Pick<AssetDraft, 'text' | 'bom' | 'eol'> {
  if (bytes.length > MAX_EDIT_BYTES) return {}
  try {
    const raw = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes)
    if (/[\x00-\x08\x0b\x0c\x0e-\x1f]/.test(raw)) return {}
    return {
      text: raw.replace(/^\uFEFF/, '').replace(/\r\n/g, '\n'),
      bom: raw.startsWith('\uFEFF'),
      eol: raw.includes('\r\n') ? '\r\n' : '\n',
    }
  } catch { return {} }
}

export function encodeAsset(draft: AssetDraft): Uint8Array {
  return new TextEncoder().encode((draft.bom ? '\uFEFF' : '') + (draft.text || '').replace(/\r\n/g, '\n').replace(/\n/g, draft.eol || '\n'))
}

export function sameBytes(a: Uint8Array, b: Uint8Array) {
  return a.length === b.length && a.every((v, i) => v === b[i])
}

export function editAsset(draft: AssetDraft, text: string): AssetDraft {
  const next = { ...draft, text }
  // Compare final bytes, including BOM and line endings, with the saved baseline.
  const baseline = next.baseBytes ? decodeAsset(next.baseBytes) : undefined
  next.changed = !next.base || !next.baseBytes || !(baseline?.text === text && baseline.bom === next.bom && baseline.eol === next.eol) && !sameBytes(encodeAsset(next), next.baseBytes)
  return next
}

export function draftBlob(draft: AssetDraft): Blob {
  if (!draft.changed && draft.baseBytes) return new Blob([draft.baseBytes as Uint8Array<ArrayBuffer>])
  if (draft.changed && draft.text !== undefined) {
    const bytes = encodeAsset(draft)
    return new Blob([bytes as Uint8Array<ArrayBuffer>])
  }
  return draft.blob || new Blob([])
}

export function dirtyAsset(draft: AssetDraft) {
  return draft.deleted ? Boolean(draft.base) : !draft.base || draft.changed
}

export function assetLanguage(path: string) {
  const ext = path.split('.').pop()?.toLowerCase()
  return ({ md: 'markdown', py: 'python', js: 'javascript', ts: 'typescript', tsx: 'typescript', jsx: 'javascript', json: 'json', yml: 'yaml', yaml: 'yaml', sh: 'shell', ps1: 'powershell', go: 'go', sql: 'sql', html: 'html', css: 'css', xml: 'xml', toml: 'ini', ini: 'ini', txt: 'plaintext' } as Record<string, string>)[ext || ''] || 'plaintext'
}

export function validAssetPath(path: string) {
  return path.length > 0 && path.length <= 1024 && !/[\\:<>"|?*\x00-\x1f\x7f]/.test(path) && path.split('/').every(part => part.trim() !== '' && part !== '.' && part !== '..' && !/[. ]$/.test(part) && !/^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\.|$)/i.test(part)) && path !== '.aie_content_hash' && (path.toLowerCase() !== 'skill.md' || path === 'SKILL.md')
}

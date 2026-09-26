import Editor, { loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor'

// 配置本地 monaco-editor，避免向外网 CDN 请求
loader.config({ monaco })

type CodeEditorProps = {
  value: string
  onChange?: (val: string) => void
  language?: string
  height?: string | number
  readOnly?: boolean
}

export function CodeEditor({
  value,
  onChange,
  language = 'yaml',
  height = '480px',
  readOnly = false,
}: CodeEditorProps) {
  return (
    <div
      style={{
        border: '1px solid #e2e8f0',
        borderRadius: '8px',
        overflow: 'hidden',
        background: '#ffffff',
      }}
    >
      <Editor
        height={height}
        language={language}
        value={value}
        onChange={(val) => onChange?.(val ?? '')}
        options={{
          readOnly,
          minimap: { enabled: false },
          fontSize: 13,
          lineNumbers: 'on',
          scrollBeyondLastLine: false,
          automaticLayout: true,
          wordWrap: 'on',
          tabSize: 2,
          renderLineHighlight: 'all',
        }}
      />
    </div>
  )
}

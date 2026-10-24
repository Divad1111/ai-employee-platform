import Editor, { loader } from '@monaco-editor/react'
import { useEffect, useRef } from 'react'
import * as monaco from 'monaco-editor'
import EditorWorker from 'monaco-editor/editor/editor.worker.js?worker'
import JsonWorker from 'monaco-editor/language/json/json.worker.js?worker'
import CssWorker from 'monaco-editor/language/css/css.worker.js?worker'
import HtmlWorker from 'monaco-editor/language/html/html.worker.js?worker'
import TypeScriptWorker from 'monaco-editor/language/typescript/ts.worker.js?worker'

// 配置本地 monaco-editor，避免向外网 CDN 请求
loader.config({ monaco })
self.MonacoEnvironment = {
  getWorker(_moduleId, label) {
    if (label === 'json') return new JsonWorker()
    if (['css', 'scss', 'less'].includes(label)) return new CssWorker()
    if (['html', 'handlebars', 'razor'].includes(label)) return new HtmlWorker()
    if (['typescript', 'javascript'].includes(label)) return new TypeScriptWorker()
    return new EditorWorker()
  },
}

type CodeEditorProps = {
  value: string
  onChange?: (val: string) => void
  language?: string
  height?: string | number
  readOnly?: boolean
}

export function CodeDiff({ original, modified, language = 'plaintext' }: { original: string; modified: string; language?: string }) {
  const container = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!container.current) return
    const before = monaco.editor.createModel(original, language)
    const after = monaco.editor.createModel(modified, language)
    const editor = monaco.editor.createDiffEditor(container.current, { readOnly: true, editContext: false, renderSideBySide: true, automaticLayout: true, minimap: { enabled: false } })
    editor.setModel({ original: before, modified: after })
    return () => {
      editor.setModel(null)
      editor.dispose()
      before.dispose()
      after.dispose()
    }
  }, [original, modified, language])
  return <div ref={container} style={{ height: 500 }} />
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
          editContext: false,
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

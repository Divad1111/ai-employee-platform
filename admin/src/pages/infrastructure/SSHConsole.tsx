import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

/** 原生 PTY 支持交互命令、设备码登录、终端尺寸变化和 Ctrl+C。 */
export function SSHConsole({
  ticket,
  onClose,
}: {
  ticket: string
  onClose: () => void
}) {
  const container = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!container.current) return
    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'Consolas, monospace',
      theme: { background: '#111827' },
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(container.current)
    fit.fit()
    const ws = new WebSocket(
      `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/infrastructure/terminal?ticket=${encodeURIComponent(ticket)}`,
    )
    ws.binaryType = 'arraybuffer'
    const send = (value: unknown) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(value))
    }
    ws.onopen = () => {
      terminal.focus()
      send({ type: 'resize', cols: terminal.cols, rows: terminal.rows })
    }
    ws.onmessage = (e) =>
      terminal.write(
        typeof e.data === 'string'
          ? e.data
          : new Uint8Array(e.data as ArrayBuffer),
      )
    ws.onerror = () =>
      terminal.writeln('\r\n终端连接失败，请检查网络或重新连接。')
    ws.onclose = () => terminal.writeln('\r\nSSH 连接已关闭。')
    const input = terminal.onData((data) => send({ type: 'input', data }))
    const observer = new ResizeObserver(() => {
      fit.fit()
      send({ type: 'resize', rows: terminal.rows, cols: terminal.cols })
    })
    observer.observe(container.current)
    const heartbeat = setInterval(() => send({ type: 'ping' }), 30000)
    return () => {
      clearInterval(heartbeat)
      observer.disconnect()
      input.dispose()
      ws.onclose = null
      ws.close()
      terminal.dispose()
    }
  }, [ticket])
  return (
    <div className="infra-terminal-wrap">
      <div
        className="infra-actions"
        style={{ padding: '0 .8rem', color: '#fff' }}
      >
        <span>SSH 终端 · 会话最长 30 分钟</span>
        <button className="btn-ghost" onClick={onClose}>
          断开连接
        </button>
      </div>
      <div className="infra-terminal" ref={container} />
    </div>
  )
}

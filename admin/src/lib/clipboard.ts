/**
 * 健壮的剪切板复制工具
 * 完美兼容 HTTPS / localhost (Clipboard API) 与局域网非安全 HTTP IP 访问 (如 http://192.168.x.x:9088，现代浏览器会禁用 navigator.clipboard)
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  if (typeof navigator !== 'undefined' && navigator.clipboard && typeof navigator.clipboard.writeText === 'function') {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // 在局域网非 HTTPS 环境可能抛出权限错误，平滑降级到 document.execCommand
    }
  }

  try {
    const textArea = document.createElement('textarea')
    textArea.value = text
    textArea.style.position = 'fixed'
    textArea.style.left = '-999999px'
    textArea.style.top = '-999999px'
    textArea.setAttribute('readonly', '')
    document.body.appendChild(textArea)
    textArea.focus()
    textArea.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(textArea)
    return ok
  } catch (err) {
    console.error('复制到剪切板失败:', err)
    return false
  }
}

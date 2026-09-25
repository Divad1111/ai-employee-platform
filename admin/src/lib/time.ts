/** 管理端统一时区：优先本机时区，读不到时用上海。 */

export const DEFAULT_TIME_ZONE = 'Asia/Shanghai'

/** 本机 IANA 时区。浏览器无法解析时返回 Asia/Shanghai。 */
export function localTimeZone(): string {
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone
    if (tz) return tz
  } catch {
    /* 部分环境没有 Intl 时区数据 */
  }
  return DEFAULT_TIME_ZONE
}

/** 中文时区名；失败时退回 IANA 标识。 */
export function timeZoneLabel(timeZone = localTimeZone()): string {
  try {
    const names = new Intl.DisplayNames(
      ['zh-CN'],
      { type: 'timeZone' } as unknown as Intl.DisplayNamesOptions,
    )
    const name = names.of(timeZone)
    if (name) return name
  } catch {
    /* 忽略 */
  }
  return timeZone
}

function dateFrom(value: string | number | Date): Date | null {
  const d = value instanceof Date ? value : new Date(value)
  return Number.isNaN(d.getTime()) ? null : d
}

/** 按本机时区格式化为日期时间。 */
export function formatDateTime(value?: string | number | Date | null): string {
  if (value == null || value === '') return '—'
  const d = dateFrom(value)
  if (!d) return String(value)
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: localTimeZone(),
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  }).format(d)
}

/** 按本机时区格式化为时分秒。 */
export function formatTime(value: string | number | Date = new Date()): string {
  const d = dateFrom(value)
  if (!d) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: localTimeZone(),
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  }).format(d)
}

/** 本机时区下的公历年月日。month 为 0–11，与 Date#getMonth 一致。 */
export function zonedYMD(value: Date = new Date()): { year: number; month: number; day: number } {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: localTimeZone(),
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(value)
  const num = (type: string) => Number(parts.find((p) => p.type === type)?.value || '0')
  return { year: num('year'), month: num('month') - 1, day: num('day') }
}

package automation

// 日历链式推进逻辑在 Service.tickCalendar / OnJobTerminal 中实现：
// 1) Tick：当日首条尚未有 active/success run 时 Fire
// 2) Job SUCCESS：Fire 同日 seq+1
// 3) Job FAILED/CANCELLED/TIMEOUT：中断链并飞书通知

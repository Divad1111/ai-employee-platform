// Package automation 实现自动化任务触发层：周期、日历链、Webhook。
// 与 scheduler（Job 派发到 Workstation）职责分离：本包只负责「何时创建 Job」。
// 设计依据：README 自动化任务需求；权限 automation.read / automation.write。
package automation

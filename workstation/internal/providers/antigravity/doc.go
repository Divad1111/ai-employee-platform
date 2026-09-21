// Package antigravity 实现 Antigravity Provider 与 Installer。
// 支持通过 Antigravity 本地 language_server / agentapi 真实调用 Antigravity Agent 执行工作；
// 同时支持离线与单元测试环境下的 FakeClient 降级。
// 设计依据：设计文档 §36、§37、§117。
package antigravity

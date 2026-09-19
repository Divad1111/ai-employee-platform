// Package acp 实现 ACP 客户端，与 Cursor/Codex 等 Agent 通信。
// 正确链路：SessionManager → Provider → ACP Client → Agent ACP Server（stdio JSON-RPC）。
// 设计依据：设计文档 §35、§117、§118；Cursor 官方：`agent acp`（禁止启动 GUI IDE）。
package acp

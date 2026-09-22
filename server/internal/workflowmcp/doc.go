// Package workflowmcp 实现「工作流MCP」领域：工作流、技能包、知识库与员工授权。
// 权限模型：仅工作流需要授权；技能与知识通过工作流引用闭包自动放行。
// 参考 PersonalWorkMCP（只读），以 PostgreSQL 为权威存储。
package workflowmcp

# MultiUser RBAC Phase 1 — §55 架构分析

> 编码前差距分析。实施以 `AI_Employee_System_MultiUser_RBAC_Design.md` §11.1/§11.3/§18/§46–§49 为准。

## 当前架构

- Center Server（Go）+ Admin-Web + Workstation 守护进程。
- Auth：`server/internal/auth`，会话 Bearer；`requirePerm` 只做权限码匹配。
- 资源：employees / workstations / jobs 等**无用户归属过滤**。

## 现有表

- `users` / `roles` / `permissions` / `user_roles` / `role_permissions`（000001）— 无 status、无 scope。
- `employees`（000002）— 无 `owner_user_id`。
- `workstations`（000003）— 无成员表。
- 最新迁移：`000012_artifacts_harden.sql`。

## 现有 API / 权限

- `/api/auth/login|logout|me`；业务路由均 `requirePerm(code)`。
- `/api/auth/me` 仅返回 id/username/roles。
- **无** `/api/users*`、成员管理、配额 API。

## 需迁移

1. `000013_multiuser_rbac.sql`：status、scope、workstation_users、employees.owner_user_id、quota 表、回填。
2. AuthZ 管道：Permission + Scope 注入 ctx；Repository 过滤。
3. 用户/角色/配额/成员 API + Admin 用户相关页。

## 访问模型定稿

- Workstation 访问：**仅** `workstation_users` 或 Scope=ALL（§11.3，不用 owner 鉴权）。
- Employee：`owner_user_id`；绑站须成员授权，否则 `WORKSTATION_ACCESS_DENIED`。

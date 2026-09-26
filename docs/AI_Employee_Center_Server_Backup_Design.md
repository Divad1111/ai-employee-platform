# AI Employee System — Center Server 备份系统设计

> 版本：v1.0  
> 目标：提供一套可直接进入开发阶段的 Center Server 全量备份系统设计。  
> 第一阶段仅实现 **Center Server 全量备份**，备份目标支持 **Local / S3 / SFTP / SMB**。  
> Workstation 备份暂不实现，但架构需要为后续扩展预留接口。

---

# 1. 目标与范围

## 1.1 本阶段目标

在现有：

```text
Center Server
Admin Web
Workstation
User
Digital Employee
Permission
```

架构基础上新增 Backup System。

第一阶段只备份 Center Server，要求：

- 备份 Center Server 的全部持久化数据
- 支持手动备份
- 支持按策略自动定时备份
- 支持备份策略
- 支持备份目标
- 支持备份记录
- 支持备份校验
- 支持备份删除
- 支持备份恢复
- 支持备份权限
- 支持备份目标连接测试
- 支持保留策略
- 支持失败重试
- 支持备份任务状态和执行日志
- 备份文件必须加密
- 备份结果必须可验证
- 为未来 Workstation / Digital Employee 单独备份预留扩展能力

## 1.2 第一阶段明确不实现

暂不实现：

- Workstation 文件备份
- Workstation Workspace 备份
- Digital Employee Workspace 单独备份
- 增量备份
- 差异备份
- 跨 Workstation 备份
- 多中心 Server 备份
- 文件级恢复
- 单 Digital Employee 恢复
- S3 版本管理
- 远程备份目标之间的数据同步

第一阶段以：

> **Center Server 全量 Snapshot Backup**

为核心。

---

# 2. 整体架构

```text
                         ┌──────────────────────┐
                         │       Admin Web      │
                         │                      │
                         │ Backup Policies      │
                         │ Destinations         │
                         │ Backup Records       │
                         │ Restore              │
                         │ Permission           │
                         └──────────┬───────────┘
                                    │
                                    ▼
                         ┌──────────────────────┐
                         │    Center Server     │
                         │                      │
                         │ Backup Manager       │
                         │ Backup Scheduler     │
                         │ Backup Executor      │
                         │ Backup Repository    │
                         │ Restore Manager      │
                         └──────────┬───────────┘
                                    │
                       ┌────────────┼────────────┐
                       │            │            │
                       ▼            ▼            ▼
                    Local         SFTP          SMB
                       │            │            │
                       │            │            │
                       └────────────┼────────────┘
                                    │
                                    ▼
                                    S3
```

逻辑组件：

```text
Backup Manager
├── Policy Manager
├── Scheduler
├── Backup Executor
├── Backup Packager
├── Backup Encryptor
├── Backup Verifier
├── Destination Manager
├── Retention Manager
└── Restore Manager
```

---

# 3. 核心设计原则

## 3.1 Backup Policy 与 Backup Destination 分离

不要把：

```text
每天 02:00
备份到 S3
```

直接写死在策略中。

使用：

```text
Backup Policy
        │
        └── Backup Destination
```

一个 Destination 可以被多个 Policy 使用。

例如：

```text
Policy: Daily Center Backup
    ├── S3-Production
    └── Local-NAS

Policy: Weekly Full Backup
    └── SFTP-Backup-Server
```

---

# 4. Center Server 全量备份内容

第一阶段要求：

> **所有需要恢复 Center Server 的持久化数据都必须进入 Backup Snapshot。**

建议备份内容至少包括：

```text
Center Server
│
├── Database
│   └── 完整数据库
│
├── Configuration
│   ├── 系统配置
│   ├── Center Server 配置
│   ├── Admin 配置
│   ├── Workstation 配置
│   ├── Digital Employee 配置
│   ├── MCP 配置
│   ├── Automation 配置
│   └── Backup 配置
│
├── User Data
│   ├── User
│   ├── Role
│   ├── Permission
│   └── User relationships
│
├── Digital Employee Data
│   ├── Employee
│   ├── Employee configuration
│   ├── Employee permissions
│   └── Employee-MCP authorization metadata
│
├── Workstation Data
│   ├── Workstation metadata
│   ├── registration information
│   ├── capabilities
│   └── configuration
│
├── Task Data
│   ├── Tasks
│   ├── execution records
│   ├── sessions
│   ├── token usage
│   └── execution timeline
│
├── Automation
│   ├── scheduled tasks
│   ├── calendar tasks
│   └── webhook configuration
│
├── MCP
│   ├── MCP definitions
│   ├── permissions
│   └── encrypted credentials metadata
│
├── Audit
│   ├── audit logs
│   └── operation records
│
└── Backup Metadata
    └── backup configuration and history
```

## 4.1 Secret / Credential 的处理

备份中可能包含：

- Feishu OAuth Token
- MCP Token
- Git Token
- SSH Key
- API Key
- 其他第三方 Credential

这些数据不能以明文进入 Backup Artifact。

要求：

```text
原始 Secret
     │
     ▼
Secret Encryption
     │
     ▼
Backup Encryption
     │
     ▼
Backup Artifact
```

Backup Artifact 本身必须加密。

---

# 5. Backup Artifact

不要直接把数据库文件和配置文件散落在目标目录。

每一次备份生成一个独立的 Snapshot。

推荐结构：

```text
backup-20260926T020000Z-01JXXXXXXXXX/
│
├── manifest.json
├── metadata.json
├── database/
│   └── database.dump
├── config/
│   └── config.bundle
├── data/
│   └── data.bundle
├── secrets/
│   └── secrets.enc
└── checksums.sha256
```

最终可以进一步打包成：

```text
backup-20260926T020000Z-01JXXXXXXXXX.backup
```

推荐：

```text
Snapshot
    ↓
Package
    ↓
Encrypt
    ↓
Checksum
    ↓
Upload
```

---

# 6. Manifest

每个 Backup 必须包含 `manifest.json`。

示例：

```json
{
  "format_version": 1,
  "backup_id": "backup-01JXXXXXXXXX",
  "created_at": "2026-09-26T02:00:00Z",
  "backup_type": "CENTER_FULL",
  "server_version": "1.0.0",
  "database_type": "postgresql",
  "encryption": {
    "algorithm": "AES-256-GCM"
  },
  "compression": {
    "algorithm": "zstd"
  },
  "files": [
    {
      "path": "database/database.dump",
      "size": 12345678,
      "sha256": "..."
    }
  ]
}
```

要求：

- Manifest 本身必须参与完整性校验
- Backup ID 全局唯一
- Manifest 中记录 Center Server 版本
- Manifest 中记录 Backup Format Version
- Manifest 中记录加密和压缩算法
- Manifest 中记录文件 checksum

---

# 7. Backup Format Version

必须设计独立的：

```text
backup_format_version
```

例如：

```text
1
2
3
```

不要直接使用数据库版本作为 Backup Format Version。

原因：

未来即使：

```text
Center Server v2
```

仍然可能需要：

```text
读取 Backup Format v1
```

恢复模块必须支持向后兼容。

---

# 8. Backup Destination

统一抽象：

```text
Backup Destination
```

支持：

```text
LOCAL
S3
SFTP
SMB
```

接口建议：

```go
type BackupDestination interface {
    Validate(ctx context.Context) error
    TestConnection(ctx context.Context) error
    Put(ctx context.Context, source BackupArtifact) error
    Exists(ctx context.Context, backupID string) (bool, error)
    Get(ctx context.Context, backupID string) (io.ReadCloser, error)
    Delete(ctx context.Context, backupID string) error
    List(ctx context.Context) ([]BackupObject, error)
    GetUsage(ctx context.Context) (*StorageUsage, error)
}
```

未来 Workstation Backup 可以继续复用。

---

# 9. Local Destination

配置：

```text
type: LOCAL
path: /data/backups
```

目录：

```text
/data/backups/
├── backup-01JXXX/
├── backup-01JYYY/
└── ...
```

要求：

- 目录必须可写
- 启动时可以检查目录
- Backup 前检查剩余空间
- 支持原子写入
- 临时文件完成后再 rename
- 避免生成半成品 Backup

推荐：

```text
backup-xxx.tmp
      ↓
完成
      ↓
backup-xxx.backup
```

---

# 10. S3 Destination

支持标准 S3 API。

配置：

```text
type: S3

endpoint
region
bucket
prefix

access_key
secret_key

path_style
```

需要兼容：

- AWS S3
- MinIO
- Cloudflare R2
- 阿里云 OSS S3 Compatible
- 腾讯云 COS S3 Compatible
- Backblaze B2 S3 Compatible
- 其他 S3 Compatible Storage

对象路径：

```text
{prefix}/
    {center_server_id}/
        {backup_id}.backup
```

例如：

```text
ai-backup/
    center-001/
        backup-01JXXX.backup
```

要求：

- 支持 HTTPS
- 支持大文件 Multipart Upload
- 上传失败支持重试
- 上传完成后验证对象存在
- 支持删除
- 支持列出 Backup
- 支持连接测试

---

# 11. SFTP Destination

配置：

```text
type: SFTP

host
port
username

authentication:
    password
    private_key

remote_path
```

建议优先：

```text
SSH Private Key
```

同时支持 Password。

目录：

```text
/backup/ai-employee/
    center-001/
        backup-01JXXX.backup
```

要求：

- SSH Host Key 校验
- 不允许默认关闭 Host Key Verification
- 支持连接测试
- 支持上传
- 支持下载
- 支持删除
- 支持列出
- 支持断线重试

---

# 12. SMB Destination

配置：

```text
type: SMB

server
share
username
password
domain
remote_path
```

例如：

```text
\\NAS01\Backup\AIEmployee
```

要求：

- 支持 SMB2/SMB3
- 不要求 SMB1
- 支持连接测试
- 支持目录创建
- 支持上传
- 支持下载
- 支持删除
- 支持列出
- 支持空间检查

---

# 13. Destination Credential 安全

Destination 可能保存：

```text
S3 Secret Key
SFTP Password
SFTP Private Key
SMB Password
```

这些不能明文保存。

推荐：

```text
Database
    │
    └── encrypted credential blob
             │
             ▼
        Secret Manager
             │
             ▼
        Encryption Key
```

如果当前系统还没有独立 Secret Manager，可以第一阶段由 Center Server 内置：

```text
Application Master Key
```

对 Credential 做：

```text
AES-256-GCM
```

数据库中只保存：

```text
ciphertext
nonce
key_version
```

Master Key 不得存储在数据库中。

建议通过环境变量 / Docker Secret / Secret File 注入。

---

# 14. Backup Policy

Backup Policy 至少包含：

```text
id
name
enabled
scope
schedule
destination_ids
retention
encryption_policy
compression
created_by
updated_by
created_at
updated_at
```

第一阶段：

```text
scope = CENTER_FULL
```

示例：

```json
{
  "name": "Daily Center Backup",
  "enabled": true,
  "scope": "CENTER_FULL",
  "schedule": {
    "type": "CRON",
    "expression": "0 2 * * *",
    "timezone": "Asia/Shanghai"
  },
  "destinations": [
    "destination-local-001",
    "destination-s3-001"
  ],
  "retention": {
    "keep_last": 7
  }
}
```

---

# 15. 调度策略

支持：

```text
Manual
Cron
```

第一阶段 Admin Web 可以提供：

- 每天
- 每周
- 每月
- 自定义 Cron

最终统一转换为：

```text
Cron Expression
Timezone
```

例如：

```text
每天 02:00
0 2 * * *

每周日 03:00
0 3 * * 0
```

必须保存：

```text
timezone
```

不能只保存 Cron。

---

# 16. Backup Execution

一次策略执行产生：

```text
Backup Run
```

例如：

```text
Policy
   │
   ▼
Backup Run #1001
   │
   ├── Destination Local
   │      └── SUCCESS
   │
   └── Destination S3
          └── SUCCESS
```

如果一个策略配置多个 Destination：

> 每个 Destination 必须有独立执行状态。

---

# 17. Backup Run 状态

建议：

```text
PENDING
RUNNING
PACKAGING
ENCRYPTING
UPLOADING
VERIFYING
SUCCESS
PARTIAL_SUCCESS
FAILED
CANCELLED
```

Destination Run：

```text
PENDING
RUNNING
UPLOADING
VERIFYING
SUCCESS
FAILED
```

---

# 18. Backup Job 防重复执行

同一个 Policy 不允许并发运行两个 Backup。

例如：

```text
02:00 Backup Run #100
```

还没结束时：

```text
02:01 Scheduler 再次触发
```

必须：

```text
跳过
```

而不是创建第二个全量备份。

建议使用数据库锁：

```text
policy_id
+
running status
```

或者分布式锁。

第一阶段如果只有单 Center Server，可以使用数据库锁。

---

# 19. Backup 全量流程

完整流程：

```text
Scheduler
    │
    ▼
Create Backup Run
    │
    ▼
Acquire Backup Lock
    │
    ▼
Create Snapshot
    │
    ├── Database Dump
    ├── Configuration
    ├── Persistent Data
    └── Secrets
    │
    ▼
Generate Manifest
    │
    ▼
Compress
    │
    ▼
Encrypt
    │
    ▼
Generate SHA-256
    │
    ▼
Create Backup Artifact
    │
    ├───────────────┐
    ▼               ▼
Destination A   Destination B
    │               │
    ▼               ▼
 Upload          Upload
    │               │
    ▼               ▼
 Verify          Verify
    │               │
    └───────┬───────┘
            ▼
      Backup Complete
            │
            ▼
      Retention Cleanup
```

---

# 20. 数据库备份

数据库必须使用数据库原生 Dump，而不是直接复制数据库文件。

例如 PostgreSQL：

```text
pg_dump
```

如果未来数据库类型发生变化：

```text
DatabaseBackupProvider
```

统一接口：

```go
type DatabaseBackupProvider interface {
    Backup(ctx context.Context, output io.Writer) error
    Restore(ctx context.Context, input io.Reader) error
}
```

第一阶段根据项目实际数据库实现。

---

# 21. 一致性要求

Backup 必须尽量保证：

```text
Database
+
Configuration
+
Persistent Data
```

属于同一个 Snapshot。

推荐：

```text
Create Backup Context
        │
        ├── Freeze/consistent read where needed
        │
        ├── Database Dump
        │
        ├── Data Snapshot
        │
        └── Config Snapshot
```

如果某些动态日志无法严格做到瞬时一致，需要在 Manifest 中记录 Snapshot 时间和组件版本。

---

# 22. 压缩

推荐：

```text
Zstandard (zstd)
```

原因：

- 压缩速度快
- 解压速度快
- 压缩率较好
- Go 生态成熟

配置：

```text
compression:
    algorithm: zstd
    level: 3
```

默认 Level 3。

---

# 23. 加密

Backup Artifact 必须加密。

建议：

```text
AES-256-GCM
```

结构：

```text
Plain Backup
      │
      ▼
Zstd
      │
      ▼
AES-256-GCM
      │
      ▼
Backup Artifact
```

密钥不能直接明文放在数据库。

---

# 24. Backup Encryption Key

建议使用 Key Version：

```text
backup_key_version = 1
```

以后轮换：

```text
version 1
version 2
version 3
```

Backup Manifest：

```json
{
  "encryption": {
    "algorithm": "AES-256-GCM",
    "key_version": 1
  }
}
```

这样以后密钥轮换后，旧 Backup 仍然可以恢复。

---

# 25. Checksum

最终 Backup Artifact 必须生成：

```text
SHA-256
```

例如：

```text
backup-01JXXX.backup
backup-01JXXX.backup.sha256
```

但是远程 Destination 不一定需要单独保存 `.sha256` 文件。

Checksum 必须同时记录到：

```text
Database Backup Record
+
Manifest
```

---

# 26. Backup Verification

上传完成以后：

```text
Upload
  ↓
Exists
  ↓
Size Check
  ↓
Download / Remote verification where supported
  ↓
SHA-256 Verify
```

对于大文件，不建议每次都完整下载远程对象验证。

第一阶段可以：

```text
上传完成
+
远程对象存在
+
大小一致
+
本地 SHA-256 已生成
```

另外提供手动：

```text
Verify Backup
```

执行完整下载 + SHA-256 校验。

---

# 27. Retention

Backup Policy 支持：

```text
keep_last
```

例如：

```text
keep_last = 7
```

表示只保留最近 7 个成功 Backup。

未来可扩展：

```text
daily
weekly
monthly
yearly
```

第一阶段只实现：

```text
Keep Last N
```

重要：

> Retention 删除必须只删除 Backup Artifact，不删除 Backup Run 审计记录。

---

# 28. 删除规则

只有：

```text
SUCCESS
FAILED
```

等已经确定生命周期状态的 Backup 才能进入清理。

不能删除：

```text
RUNNING
UPLOADING
VERIFYING
```

中的 Backup。

---

# 29. 备份权限

新增独立 Backup Permission。

建议权限：

```text
backup:view
backup:create
backup:manage
backup:restore
backup:delete
backup:destination
backup:policy
backup:verify
```

## 29.1 权限说明

### backup:view

查看：

- Backup Policy
- Destination
- Backup Records
- Backup 状态

### backup:create

允许：

- 手动执行 Backup

### backup:manage

允许：

- 创建 Policy
- 修改 Policy
- 启停 Policy
- 修改 Retention

### backup:restore

允许：

- 创建 Restore Job
- 执行恢复

### backup:delete

允许：

- 删除 Backup
- 手动触发清理

### backup:destination

允许：

- 创建 Destination
- 修改 Destination
- 删除 Destination
- 测试 Destination

### backup:policy

如果系统已有细粒度管理权限，可以与 `backup:manage` 合并。

### backup:verify

允许：

- 手动校验 Backup

---

# 30. 推荐权限最小集合

如果现有权限系统不适合太细，可以第一版使用：

```text
backup.view
backup.create
backup.manage
backup.restore
backup.delete
```

其中：

```text
backup.manage
```

包括：

- Policy
- Destination
- Retention
- Scheduler

但是建议底层 Permission Code 仍然保留细粒度扩展能力。

---

# 31. 权限原则

权限应该属于：

```text
User
   ↓
Role
   ↓
Permission
```

而不是：

```text
Workstation
   ↓
Backup Permission
```

因为第一阶段只备份 Center Server。

未来 Workstation Backup 再扩展：

```text
backup.center
backup.workstation
backup.employee
```

---

# 32. Admin Web 页面

建议新增：

```text
系统管理
└── 备份管理
    ├── 总览
    ├── 备份策略
    ├── 存储目标
    ├── 备份记录
    └── 恢复任务
```

---

# 33. Backup Dashboard

显示：

```text
备份总数
成功次数
失败次数
最近一次备份
最近一次备份状态
最近一次成功备份
存储空间
备份策略数量
```

例如：

```text
┌──────────────────────────────────────────┐
│ Backup Overview                          │
├──────────┬──────────┬──────────┬─────────┤
│ 策略     │ 总备份   │ 成功     │ 失败    │
│ 3        │ 128      │ 126      │ 2       │
└──────────┴──────────┴──────────┴─────────┘

最近备份
2026-09-26 02:00
SUCCESS
18.2 GB
```

---

# 34. Backup Policy 页面

列表：

```text
名称
状态
备份范围
Schedule
Destination
Retention
最后执行
下次执行
操作
```

操作：

```text
启用
禁用
立即执行
编辑
删除
查看记录
```

---

# 35. 创建 Policy

表单：

```text
名称

备份范围：
Center Server 全量

执行：
○ 手动
○ 定时

Schedule：
每天
时间：
02:00
时区：
Asia/Shanghai

备份目标：
☑ Local
☑ S3

Retention：
保留最近：
7
个
```

---

# 36. Destination 页面

列表：

```text
名称
类型
状态
地址
最后测试
最后使用
操作
```

操作：

```text
测试连接
编辑
删除
```

新增：

```text
Local
S3
SFTP
SMB
```

---

# 37. Destination 测试

测试连接必须真正执行：

```text
Connect
    ↓
Authenticate
    ↓
Check Permission
    ↓
Check Path
    ↓
Check Write
    ↓
Check Delete Temporary File
```

最终：

```text
Connection Test: SUCCESS
```

而不是只测试 TCP。

---

# 38. Backup Record

列表：

```text
Backup ID
Policy
Created At
Duration
Size
Destination
Status
Checksum
```

点击进入：

```text
Backup Detail
```

显示：

```text
Backup ID
创建时间
开始时间
完成时间
耗时
大小
文件数
SHA-256
Server Version
Backup Format Version
Encryption Version
Compression
```

Destination：

```text
Local    SUCCESS
S3       SUCCESS
SFTP     FAILED
```

---

# 39. Restore

第一阶段支持：

> **Center Server 整体恢复**

不要做文件级恢复。

流程：

```text
选择 Backup
      ↓
确认 Restore
      ↓
验证 Backup
      ↓
停止相关服务
      ↓
恢复 Database
      ↓
恢复 Configuration
      ↓
恢复 Persistent Data
      ↓
恢复 Secrets
      ↓
启动服务
      ↓
Health Check
```

Restore 是高风险操作。

必须要求：

```text
backup.restore
```

权限。

建议 Admin Web 二次确认。

---

# 40. Restore 安全措施

恢复前：

```text
Create Current State Backup
```

即：

```text
Current Center
       │
       ▼
Emergency Backup
       │
       ▼
Restore Selected Backup
```

避免恢复失败后完全无法回滚。

---

# 41. Restore 不能直接覆盖运行中的数据库

禁止：

```text
Backup
   ↓
直接覆盖 PostgreSQL 数据目录
```

应该：

```text
Stop Application
       ↓
Backup Current State
       ↓
Restore DB
       ↓
Restore Data
       ↓
Validate
       ↓
Start
```

具体实现由数据库类型决定。

---

# 42. Backup Database Model

建议新增：

## backup_destinations

```text
id
name
type
config_encrypted
enabled
created_by
updated_by
created_at
updated_at
last_test_at
last_test_status
```

注意：

`config_encrypted` 中包含敏感信息。

---

## backup_policies

```text
id
name
enabled
scope
schedule_type
cron_expression
timezone
retention_keep_last
compression_algorithm
compression_level
encryption_key_version
created_by
updated_by
created_at
updated_at
```

---

## backup_policy_destinations

```text
policy_id
destination_id
created_at
```

联合唯一：

```text
(policy_id, destination_id)
```

---

## backup_runs

```text
id
policy_id
backup_id
scope
status
started_at
completed_at
duration_ms
artifact_size
file_count
checksum
error_code
error_message
created_at
```

---

## backup_run_destinations

```text
id
backup_run_id
destination_id
status
remote_path
remote_size
uploaded_at
verified_at
error_code
error_message
```

---

## backup_restore_jobs

```text
id
backup_run_id
status
started_at
completed_at
created_by
error_code
error_message
created_at
```

---

## backup_audit_logs

如果系统已有统一 Audit Log，则不需要单独建表。

所有：

```text
创建 Policy
修改 Policy
删除 Policy
创建 Destination
修改 Destination
删除 Destination
手动 Backup
删除 Backup
Verify
Restore
```

必须写入系统 Audit Log。

---

# 43. Backup ID

Backup ID 必须全局唯一。

推荐：

```text
UUIDv7
```

或者：

```text
ULID
```

例如：

```text
01K6XXXXXX...
```

不要使用：

```text
timestamp
```

作为唯一 ID。

---

# 44. Backup 文件命名

推荐：

```text
{backup_id}.backup
```

例如：

```text
01K6ABCXYZ.backup
```

不要把用户可控名称直接放入路径。

---

# 45. 原子上传

所有 Destination 都应该遵循：

```text
temporary object/file
        ↓
upload
        ↓
verify
        ↓
rename / finalize
```

例如：

```text
01K6XXX.backup.uploading
```

完成：

```text
01K6XXX.backup
```

避免服务崩溃留下看起来完整的 Backup。

---

# 46. 重试策略

远程上传失败支持自动重试：

```text
Attempt 1
   ↓
5s
   ↓
Attempt 2
   ↓
30s
   ↓
Attempt 3
   ↓
5min
```

推荐：

```text
max_retries = 3
```

使用 Exponential Backoff + Jitter。

永久错误不应该无限重试，例如：

```text
Authentication Failed
Permission Denied
Invalid Bucket
Invalid Path
```

---

# 47. 并发策略

一个 Backup Artifact 生成一次。

然后：

```text
Artifact
   ├── S3 upload
   ├── SFTP upload
   ├── SMB upload
   └── Local copy
```

可以并行上传。

但是需要限制：

```text
max_concurrent_destinations
```

第一版默认：

```text
2
```

避免多个远程上传同时消耗过多资源。

---

# 48. 临时目录

Center Server 使用：

```text
{data_dir}/backup/work/
```

例如：

```text
backup/
├── work/
│   └── backup-01JXXX/
├── cache/
└── ...
```

Backup 完成后必须清理临时文件。

服务异常后启动时：

```text
scan work/
```

清理超过 TTL 的临时目录。

例如：

```text
TTL = 24h
```

---

# 49. 磁盘空间保护

Backup 开始前检查：

```text
available_disk_space
```

要求：

```text
available >= estimated_backup_size * safety_factor
```

如果无法估算：

```text
至少保留 10GB
```

具体值可以配置。

如果空间不足：

```text
BACKUP_INSUFFICIENT_STORAGE
```

并进入 FAILED。

---

# 50. 日志

Backup 日志必须结构化。

例如：

```json
{
  "event": "backup.upload",
  "backup_id": "01K6XXX",
  "destination_id": "s3-001",
  "status": "success",
  "duration_ms": 18230,
  "size": 192837465
}
```

禁止在日志中打印：

```text
S3 Secret Key
SSH Private Key
Password
OAuth Token
MCP Token
```

---

# 51. API 设计

建议 REST API：

```text
GET    /api/admin/backups/policies
POST   /api/admin/backups/policies
GET    /api/admin/backups/policies/{id}
PUT    /api/admin/backups/policies/{id}
DELETE /api/admin/backups/policies/{id}

POST   /api/admin/backups/policies/{id}/run
POST   /api/admin/backups/policies/{id}/enable
POST   /api/admin/backups/policies/{id}/disable

GET    /api/admin/backups/destinations
POST   /api/admin/backups/destinations
GET    /api/admin/backups/destinations/{id}
PUT    /api/admin/backups/destinations/{id}
DELETE /api/admin/backups/destinations/{id}

POST   /api/admin/backups/destinations/{id}/test

GET    /api/admin/backups/runs
GET    /api/admin/backups/runs/{id}

POST   /api/admin/backups/runs/{id}/verify
DELETE /api/admin/backups/runs/{id}

POST   /api/admin/backups/runs/{id}/restore
GET    /api/admin/backups/restore-jobs
GET    /api/admin/backups/restore-jobs/{id}
```

API 必须经过现有 Admin Authentication + Permission Middleware。

---

# 52. Scheduler

Scheduler 运行在 Center Server。

启动：

```text
Center Server
    │
    ├── HTTP Server
    ├── Business Services
    ├── Database
    ├── Backup Manager
    └── Backup Scheduler
```

不需要单独部署 Backup Server。

第一阶段：

```text
Backup Scheduler
```

直接运行在 Center Server 进程中即可。

如果未来任务规模增加，可以拆成独立 Worker。

---

# 53. Scheduler 防止重复触发

需要支持：

```text
Policy Enabled
+
Current Time >= Next Run
+
No Running Run
```

才创建 Backup Run。

Backup Run 创建必须使用数据库事务。

---

# 54. API 与后台任务解耦

用户点击：

```text
立即备份
```

API 不应该等待 Backup 完成。

应该：

```text
POST /run
      ↓
创建 Backup Run
      ↓
return 202
      ↓
Background Worker 执行
```

返回：

```json
{
  "backup_run_id": "01K6XXX",
  "status": "PENDING"
}
```

Admin Web 通过：

```text
GET /backup/runs/{id}
```

查看进度。

---

# 55. Backup Progress

可以提供：

```text
CREATED
SNAPSHOT
PACKAGING
ENCRYPTING
UPLOADING
VERIFYING
COMPLETED
```

以及：

```text
current_destination
uploaded_bytes
total_bytes
percent
```

如果远程协议无法可靠提供进度，允许：

```text
progress = null
```

不要伪造百分比。

---

# 56. 权限初始化

数据库 Migration 中增加默认 Permission：

```text
backup.view
backup.create
backup.manage
backup.restore
backup.delete
backup.destination
backup.verify
```

如果系统已有超级管理员角色：

> 超级管理员默认获得全部 Backup 权限。

普通用户默认不获得 Backup 权限。

---

# 57. 默认安全策略

新建 Backup Destination：

```text
enabled = false
```

直到：

```text
Test Connection
+
Test Success
```

用户显式启用。

新建 Policy：

```text
enabled = false
```

直到用户显式启用。

---

# 58. 删除 Destination 的保护

如果 Destination 正被 Policy 使用：

```text
Policy A
   ↓
Destination S3
```

不允许直接删除。

返回：

```text
DESTINATION_IN_USE
```

要求：

```text
先解除 Policy 绑定
```

---

# 59. 删除 Policy

删除 Policy 不应该删除历史 Backup。

即：

```text
Delete Policy
     ↓
Policy 删除
     ↓
Backup Records 保留
     ↓
Backup Artifacts 保留
```

---

# 60. 删除 Backup

手动删除 Backup：

```text
backup.delete
```

必须：

1. 删除 Destination 上的 Artifact
2. 确认所有 Destination 删除结果
3. 更新 Backup Record
4. 写 Audit Log

如果某个 Destination 删除失败：

```text
PARTIAL_DELETE
```

不能假装完全成功。

---

# 61. 多 Destination 语义

一个 Policy：

```text
Daily Backup
├── Local
└── S3
```

应该视为：

```text
一个 Backup Run
+
两个 Destination Run
```

整体状态：

```text
Local SUCCESS
S3 SUCCESS
    ↓
Backup Run SUCCESS
```

如果：

```text
Local SUCCESS
S3 FAILED
```

整体：

```text
PARTIAL_SUCCESS
```

---

# 62. “备份成功”的定义

Policy Run 只有在：

```text
至少一个 Destination SUCCESS
```

时才可以认为 Artifact 已经成功生成。

但是：

```text
全部 Destination SUCCESS
```

才是：

```text
SUCCESS
```

如果只有部分：

```text
PARTIAL_SUCCESS
```

---

# 63. 备份状态与系统健康状态

Backup 失败不能直接让整个 Center Server：

```text
HEALTH = DOWN
```

但是系统健康页面应该显示：

```text
Backup Status: WARNING
Last Successful Backup: 26 hours ago
```

未来可以增加：

```text
Backup SLA
```

例如：

```text
超过 24 小时没有成功 Backup
→ WARNING
```

---

# 64. 安全要求

必须满足：

- 所有 Admin API 使用现有认证
- 所有 Backup API 使用 Permission
- Destination Credential 加密
- Backup Artifact 加密
- Secret 不写日志
- SFTP 校验 Host Key
- S3 使用 HTTPS
- SMB 使用 SMB2/SMB3
- Restore 需要高权限
- 删除 Backup 需要权限
- 所有管理操作写 Audit Log

---

# 65. 防止路径穿越

Local / SFTP / SMB 都不能直接信任用户提供的路径。

禁止：

```text
../../
```

以及：

```text
绝对路径越界
```

必须经过规范化：

```text
Clean
Normalize
Validate
```

并确保最终路径位于用户配置的 Destination Root 内。

---

# 66. Backup Artifact 不信任原则

恢复 Backup 时：

> Backup 中的路径、文件名、配置内容都属于不可信输入。

必须防止：

```text
../
绝对路径
符号链接逃逸
恶意文件覆盖
```

Restore 时：

```text
Archive Entry
     ↓
Validate Path
     ↓
Validate Type
     ↓
Extract
```

---

# 67. 备份恢复后的版本检查

恢复前检查：

```text
Backup Format Version
Center Server Version
Database Schema Version
```

如果当前版本不支持：

```text
RESTORE_VERSION_INCOMPATIBLE
```

禁止直接恢复。

如果支持迁移：

```text
Restore
   ↓
Migration
```

必须明确记录。

---

# 68. 推荐代码结构

如果 Center Server 使用 Go，建议：

```text
internal/
├── backup/
│   ├── manager.go
│   ├── scheduler.go
│   ├── executor.go
│   ├── snapshot.go
│   ├── package.go
│   ├── encrypt.go
│   ├── verify.go
│   ├── retention.go
│   ├── restore.go
│   │
│   ├── destination/
│   │   ├── interface.go
│   │   ├── local.go
│   │   ├── s3.go
│   │   ├── sftp.go
│   │   └── smb.go
│   │
│   ├── database/
│   │   ├── interface.go
│   │   └── postgres.go
│   │
│   └── model/
│       ├── policy.go
│       ├── destination.go
│       ├── run.go
│       └── restore.go
│
├── permission/
├── audit/
└── ...
```

具体目录需要适配现有 Center Server 项目结构，不要求机械照搬。

---

# 69. 推荐开发顺序

## Phase 1 — Database

实现：

```text
backup_destinations
backup_policies
backup_policy_destinations
backup_runs
backup_run_destinations
backup_restore_jobs
```

以及 Permission Migration。

---

## Phase 2 — Destination

先实现：

```text
Local
```

然后：

```text
S3
SFTP
SMB
```

每个 Destination 都实现统一接口。

---

## Phase 3 — Backup Engine

实现：

```text
Snapshot
Packaging
Compression
Encryption
Checksum
Verification
```

---

## Phase 4 — Scheduler

实现：

```text
Cron
Timezone
Policy
Lock
Retry
```

---

## Phase 5 — Admin API

实现：

```text
Policy CRUD
Destination CRUD
Run
Verify
Delete
Restore
```

---

## Phase 6 — Admin Web

实现：

```text
Dashboard
Policy
Destination
Backup Records
Backup Detail
Restore
```

---

## Phase 7 — Testing

必须测试：

### Local

```text
Backup
Verify
Restore
Delete
```

### S3

```text
Connect
Upload
Verify
Download
Delete
```

### SFTP

```text
Connect
Upload
Download
Delete
```

### SMB

```text
Connect
Upload
Download
Delete
```

---

# 70. 故障测试

必须覆盖：

```text
磁盘空间不足
网络断开
S3 Authentication 失败
SFTP Authentication 失败
SMB Authentication 失败
上传中断
Center Server 重启
Backup Worker 崩溃
数据库 Dump 失败
加密失败
Checksum 不一致
Remote Object 不存在
Destination 权限不足
Backup 重复执行
Restore 失败
Restore 中途进程退出
```

---

# 71. 核心验收标准

## 备份

可以：

```text
创建 Backup Policy
       ↓
配置每天 02:00
       ↓
选择 S3
       ↓
自动执行
       ↓
生成完整 Center Backup
       ↓
上传 S3
       ↓
Verify
       ↓
SUCCESS
```

---

## 多目标

配置：

```text
Local
S3
SFTP
SMB
```

一次 Backup：

```text
生成一次 Artifact
       ↓
分别上传四个 Destination
```

---

## 权限

没有：

```text
backup.create
```

不能手动执行 Backup。

没有：

```text
backup.manage
```

不能修改策略。

没有：

```text
backup.destination
```

不能管理 Destination。

没有：

```text
backup.restore
```

不能恢复。

没有：

```text
backup.delete
```

不能删除 Backup。

---

# 72. 未来扩展

当前：

```text
CENTER_FULL
```

未来可以增加：

```text
CENTER_FULL
CENTER_INCREMENTAL

WORKSTATION_FULL
WORKSTATION_INCREMENTAL

EMPLOYEE_WORKSPACE
```

未来架构：

```text
Backup Scope
├── CENTER
├── WORKSTATION
└── DIGITAL_EMPLOYEE
```

但第一阶段不要提前实现这些功能。

---

# 73. 未来 Workstation Backup 的扩展点

未来：

```text
Center Server
      │
      │ Backup Command
      ▼
Workstation
      │
      ├── Workspace
      ├── Employee Data
      └── Local Data
      │
      ▼
Backup Artifact
      │
      ▼
Destination
```

Workstation 不需要自己重新实现 Destination。

可以复用：

```text
Backup Artifact
Backup Encryption
Backup Manifest
Destination Interface
Retention
Verification
```

---

# 74. 最终系统结构

```text
                         ┌──────────────────────────┐
                         │        Admin Web         │
                         │                          │
                         │ Backup Dashboard         │
                         │ Backup Policies          │
                         │ Backup Destinations      │
                         │ Backup Records           │
                         │ Restore                  │
                         └────────────┬─────────────┘
                                      │
                               REST / Permission
                                      │
                                      ▼
┌───────────────────────────────────────────────────────────────┐
│                       Center Server                           │
│                                                               │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │                    Backup Manager                       │  │
│  │                                                         │  │
│  │ Policy Manager                                         │  │
│  │ Scheduler                                               │  │
│  │ Executor                                                │  │
│  │ Snapshot                                                │  │
│  │ Package                                                 │  │
│  │ Encrypt                                                 │  │
│  │ Verify                                                  │  │
│  │ Retention                                               │  │
│  │ Restore                                                 │  │
│  └───────────────────────┬─────────────────────────────────┘  │
│                          │                                    │
│              ┌───────────┼───────────┐                        │
│              ▼           ▼           ▼                        │
│           Database   Configuration  Persistent Data           │
│              │           │           │                        │
│              └───────────┼───────────┘                        │
│                          ▼                                    │
│                    Backup Artifact                            │
│                          │                                    │
│          ┌───────────────┼────────────────┐                   │
│          ▼               ▼                ▼                   │
│        Local            S3              SFTP / SMB             │
└───────────────────────────────────────────────────────────────┘
```

---

# 75. 给 Codex 的实现要求

实现时必须遵循：

1. **先阅读现有 Center Server、Admin Web、User/Role/Permission、Audit Log、Database 代码。**
2. 不要重新创建一套 Authentication。
3. 不要重新创建一套 Permission System。
4. 不要重新创建一套 Audit System。
5. Backup API 必须接入现有 Admin Authentication。
6. Backup API 必须接入现有 Permission Middleware。
7. Backup 操作必须写入现有 Audit Log。
8. Backup Destination Credential 必须加密存储。
9. Backup Artifact 必须加密。
10. 不允许 Secret 出现在日志中。
11. Backup 必须支持失败重试。
12. Backup 必须支持 Verify。
13. Backup 必须支持 Retention。
14. 同一个 Policy 不允许并发执行。
15. Backup API 必须异步执行，不能阻塞 HTTP Request。
16. 一个 Backup Run 只生成一次 Artifact，然后上传到多个 Destination。
17. Destination 必须通过统一接口抽象。
18. Local / S3 / SFTP / SMB 都必须实现统一 Destination Interface。
19. Restore 必须有独立权限。
20. Restore 前必须执行安全检查。
21. Restore 必须记录 Audit Log。
22. 不允许通过路径字符串直接覆盖任意文件。
23. 所有 Archive Entry 都必须防止 Path Traversal。
24. Backup Format 必须有独立版本号。
25. 为未来 Workstation Backup 保留扩展接口，但第一阶段不要实现 Workstation Backup。
26. 不要实现增量 Backup，第一阶段全部使用 Center Server Full Backup。
27. 不要为了“未来扩展”提前增加复杂功能，保持第一阶段实现简单、可靠、可测试。

---

# 76. 第一阶段最终功能清单

```text
[权限]
☑ backup.view
☑ backup.create
☑ backup.manage
☑ backup.restore
☑ backup.delete
☑ backup.destination
☑ backup.verify

[策略]
☑ 创建
☑ 修改
☑ 删除
☑ 启用
☑ 禁用
☑ Cron
☑ Timezone
☑ Keep Last N
☑ 手动执行

[Destination]
☑ Local
☑ S3
☑ SFTP
☑ SMB
☑ Test Connection
☑ Credential Encryption

[Backup]
☑ Center Server Full Backup
☑ Database Dump
☑ Configuration
☑ Persistent Data
☑ Secrets
☑ Compression
☑ Encryption
☑ SHA-256
☑ Manifest
☑ Upload
☑ Verify
☑ Retry
☑ Retention

[记录]
☑ Backup Run
☑ Destination Run
☑ Progress
☑ Error
☑ Audit Log

[Restore]
☑ Center Server Full Restore
☑ Restore Permission
☑ Pre-Restore Emergency Backup
☑ Verify
☑ Restore
☑ Health Check

[安全]
☑ Secret Encryption
☑ Artifact Encryption
☑ Path Traversal Protection
☑ SFTP Host Key Verification
☑ SMB2/SMB3
☑ HTTPS S3
☑ Audit
```

---

# 77. 实现优先级

### P0 — 必须完成

```text
Permission
Database Migration
Backup Policy
Local Destination
Backup Engine
Database Backup
Encryption
Manifest
Checksum
Backup Run
Audit Log
Scheduler
```

### P1 — 必须完成

```text
S3
SFTP
SMB
Retention
Verify
Admin Web
```

### P2 — 第一阶段建议完成

```text
Restore
Pre-Restore Backup
Dashboard
Progress
Retry
```

---

# 78. Definition of Done

当以下场景全部通过时，认为第一阶段完成：

```text
1. Admin 创建 Local Destination
2. Test Connection 成功
3. 创建 Center Full Backup Policy
4. 给 Policy 配置每天 02:00
5. 手动 Run Backup
6. Center Server 生成完整 Backup Artifact
7. Backup Artifact 加密
8. Manifest 正确生成
9. SHA-256 正确生成
10. Artifact 上传 Local 成功
11. Verify 成功
12. Admin 可以看到 Backup Record
13. Retention 正确删除旧 Backup
14. 创建 S3 Destination
15. S3 Upload 成功
16. S3 Verify 成功
17. 创建 SFTP Destination
18. SFTP Upload 成功
19. SFTP Verify 成功
20. 创建 SMB Destination
21. SMB Upload 成功
22. SMB Verify 成功
23. 模拟网络失败，自动 Retry
24. 模拟 Destination Auth 失败，正确 FAILED
25. 模拟 Backup 并发，第二个 Run 被阻止
26. 删除 Backup 正确执行
27. Restore 前自动生成 Emergency Backup
28. Restore 成功
29. Restore 后 Health Check 成功
30. 所有操作写入 Audit Log
31. 无权限用户无法执行对应操作
32. 日志中不存在任何 Secret
```

---

# 79. 设计结论

第一阶段采用：

```text
Center Server Full Backup
        │
        ▼
Backup Policy
        │
        ▼
Backup Manager
        │
        ▼
Snapshot
        │
        ▼
Zstd
        │
        ▼
AES-256-GCM
        │
        ▼
SHA-256
        │
        ├──────────────┬──────────────┬──────────────┐
        ▼              ▼              ▼              ▼
      Local           S3            SFTP           SMB
```

权限采用：

```text
User
  ↓
Role
  ↓
Backup Permissions
```

远程目标统一采用：

```text
BackupDestination Interface
```

第一阶段严格限定：

```text
CENTER_FULL
```

不实现 Workstation Backup、不实现增量 Backup。

但在接口、Backup Scope、Artifact Format、Destination Interface 上预留未来：

```text
WORKSTATION
DIGITAL_EMPLOYEE
INCREMENTAL
```

的扩展能力。

这套结构可以直接作为 Center Server + Admin Web 的 Backup System 开发规格。

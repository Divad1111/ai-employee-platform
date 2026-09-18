# PostgreSQL Migrations
#
# 禁止应用启动时自动改生产 schema。
# 使用 server/cmd/migrate 执行本目录脚本。
# 设计依据：设计文档 §104。
#
# 命名建议：000001_xxx.up.sql / 000001_xxx.down.sql

// Package database 封装 PostgreSQL 访问与迁移协作。
// 业务模块通过本包获取连接，禁止在业务层直接拼 SQL 驱动细节。
// 设计依据：设计文档 §26、§104。
package database

// 占位：M1 引入 pgx / database/sql 与 migrate runner。

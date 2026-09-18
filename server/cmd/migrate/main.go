// Package main 是数据库迁移命令行入口。
// 禁止在应用启动时偷偷修改 schema；一律通过本命令执行 migrations。
// 设计依据：设计文档 §104。
// 工具：pressly/goose（见 docs/STACK.md）。
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	defaultDir := defaultMigrationsDir()
	var (
		dir = flag.String("dir", defaultDir, "migrations 目录")
		dsn = flag.String("dsn", getenv("AIE_DATABASE_URL", "postgres://aie:aie@localhost:5432/aie?sslmode=disable"), "PostgreSQL DSN")
	)
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "用法: migrate [-dir path] [-dsn url] <up|down|status|version>\n")
		os.Exit(2)
	}
	cmd := args[0]

	db, err := sql.Open("pgx", *dsn)
	if err != nil {
		fatal("打开数据库失败: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fatal("连接数据库失败: %v", err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		fatal("设置 dialect 失败: %v", err)
	}

	switch cmd {
	case "up":
		err = goose.Up(db, *dir)
	case "down":
		err = goose.Down(db, *dir)
	case "status":
		err = goose.Status(db, *dir)
	case "version":
		var v int64
		v, err = goose.GetDBVersion(db)
		if err == nil {
			fmt.Printf("version: %d\n", v)
		}
	default:
		fatal("未知子命令: %s", cmd)
	}
	if err != nil {
		fatal("migrate %s 失败: %v", cmd, err)
	}
	fmt.Printf("migrate %s 完成 (dir=%s)\n", cmd, *dir)
}

// defaultMigrationsDir 猜测仓库根下的 migrations 路径。
func defaultMigrationsDir() string {
	candidates := []string{
		"migrations",
		"../migrations",
		"../../migrations",
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return "migrations"
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

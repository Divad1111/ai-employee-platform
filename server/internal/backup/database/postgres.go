package database

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// DatabaseBackupProvider 统一数据库备份与恢复接口
type DatabaseBackupProvider interface {
	Backup(ctx context.Context, output io.Writer) error
	Restore(ctx context.Context, input io.Reader) error
}

// PostgresProvider 纯 Go 实现的 PostgreSQL 备份与恢复引擎
// 零外部 CLI 依赖，适配容器及不同操作系统环境
type PostgresProvider struct {
	db *sql.DB
}

// NewPostgresProvider 初始化
func NewPostgresProvider(db *sql.DB) *PostgresProvider {
	return &PostgresProvider{db: db}
}

// TableDump 单表导出数据
type TableDump struct {
	Name        string   `json:"name"`
	Columns     []string `json:"columns"`
	ColumnTypes []string `json:"column_types,omitempty"`
	Rows        [][]any  `json:"rows"`
}

// DatabaseDump 全库导出结构
type DatabaseDump struct {
	FormatVersion int         `json:"format_version"`
	Tables        []TableDump `json:"tables"`
	Sequences     []SeqDump   `json:"sequences,omitempty"`
}

type SeqDump struct {
	Name      string `json:"name"`
	LastValue int64  `json:"last_value"`
}

// Backup 启动只读快照事务导出所有 public 用户表
func (p *PostgresProvider) Backup(ctx context.Context, output io.Writer) error {
	if p.db == nil {
		return fmt.Errorf("数据库连接不可用")
	}

	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return fmt.Errorf("启动快照事务失败: %w", err)
	}
	defer tx.Rollback()

	// 1. 获取所有 public 表
	rows, err := tx.QueryContext(ctx, `
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = 'public' 
		  AND table_type = 'BASE TABLE'
		ORDER BY table_name ASC;
	`)
	if err != nil {
		return fmt.Errorf("查询数据库表列表失败: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		// 备份表排除 backup_runs / backup_run_destinations / backup_restore_jobs
		// 防止恢复旧快照时抹除当前正在执行的任务状态、容灾历史及恢复前刚创建的应急快照
		if name == "backup_runs" || name == "backup_run_destinations" || name == "backup_restore_jobs" {
			continue
		}
		tableNames = append(tableNames, name)
	}
	rows.Close()

	dump := DatabaseDump{
		FormatVersion: 1,
		Tables:        make([]TableDump, 0, len(tableNames)),
	}

	// 2. 依次导出每张表
	for _, tName := range tableNames {
		tDump, err := p.dumpTable(ctx, tx, tName)
		if err != nil {
			return fmt.Errorf("导出表 %s 失败: %w", tName, err)
		}
		dump.Tables = append(dump.Tables, *tDump)
	}

	// 3. 导出序列当前值（若有）
	seqRows, err := tx.QueryContext(ctx, `
		SELECT sequence_name 
		FROM information_schema.sequences 
		WHERE sequence_schema = 'public';
	`)
	if err == nil {
		defer seqRows.Close()
		for seqRows.Next() {
			var sName string
			if err := seqRows.Scan(&sName); err == nil {
				var val int64
				q := fmt.Sprintf(`SELECT last_value FROM %q`, sName)
				if err := tx.QueryRowContext(ctx, q).Scan(&val); err == nil {
					dump.Sequences = append(dump.Sequences, SeqDump{Name: sName, LastValue: val})
				}
			}
		}
		seqRows.Close()
	}

	enc := json.NewEncoder(output)
	return enc.Encode(&dump)
}

func (p *PostgresProvider) dumpTable(ctx context.Context, tx *sql.Tx, tableName string) (*TableDump, error) {
	query := fmt.Sprintf(`SELECT * FROM %q`, tableName)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	cols := make([]string, len(colTypes))
	typeNames := make([]string, len(colTypes))
	for i, ct := range colTypes {
		cols[i] = ct.Name()
		typeNames[i] = strings.ToUpper(ct.DatabaseTypeName())
	}

	td := &TableDump{
		Name:        tableName,
		Columns:     cols,
		ColumnTypes: typeNames,
		Rows:        make([][]any, 0),
	}

	for rows.Next() {
		colVals := make([]any, len(cols))
		colPointers := make([]any, len(cols))
		for i := range colVals {
			colPointers[i] = &colVals[i]
		}
		if err := rows.Scan(colPointers...); err != nil {
			return nil, err
		}

		// 格式化处理字节数组与类型转换
		row := make([]any, len(cols))
		for i, val := range colVals {
			if val == nil {
				row[i] = nil
				continue
			}
			switch v := val.(type) {
			case []byte:
				// 若为 BYTEA 二进制列，以标准 PostgreSQL 十六进制字面量 "\x..." 存储
				// 避免普通字符串转换导致非 UTF-8 乱码或还原时报 22P02 语法错误
				if typeNames[i] == "BYTEA" {
					row[i] = "\\x" + hex.EncodeToString(v)
				} else {
					row[i] = string(v)
				}
			default:
				row[i] = v
			}
		}
		td.Rows = append(td.Rows, row)
	}

	return td, rows.Err()
}

// Restore 原子恢复数据库
func (p *PostgresProvider) Restore(ctx context.Context, input io.Reader) error {
	if p.db == nil {
		return fmt.Errorf("数据库连接不可用")
	}

	var dump DatabaseDump
	if err := json.NewDecoder(input).Decode(&dump); err != nil {
		return fmt.Errorf("解析数据库备份快照失败: %w", err)
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启恢复事务失败: %w", err)
	}
	defer tx.Rollback()

	// 禁用外键约束与触发器，允许按任意表序还原
	if _, err := tx.ExecContext(ctx, "SET session_replication_role = 'replica';"); err != nil {
		return fmt.Errorf("设置 session_replication_role 失败: %w", err)
	}

	// 1. 先一次性清空所有待还原表，避免在还原过程中单独 TRUNCATE CASCADE 级联冲刷掉已还原的子表数据
	var tablesToTruncate []string
	for _, td := range dump.Tables {
		if td.Name == "backup_runs" || td.Name == "backup_run_destinations" || td.Name == "backup_restore_jobs" {
			continue
		}
		tablesToTruncate = append(tablesToTruncate, fmt.Sprintf("%q", td.Name))
	}
	if len(tablesToTruncate) > 0 {
		truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s CASCADE;", strings.Join(tablesToTruncate, ", "))
		if _, err := tx.ExecContext(ctx, truncateSQL); err != nil {
			return fmt.Errorf("清空待还原表失败: %w", err)
		}
	}

	for _, td := range dump.Tables {
		// 跳过备份系统元数据表，防止还原旧快照时抹除当前正在执行的任务状态、容灾历史及恢复前刚创建的应急快照
		if td.Name == "backup_runs" || td.Name == "backup_run_destinations" || td.Name == "backup_restore_jobs" {
			continue
		}

		if len(td.Rows) == 0 {
			continue
		}

		// 构建批量插入语句
		colList := make([]string, len(td.Columns))
		for i, col := range td.Columns {
			colList[i] = fmt.Sprintf("%q", col)
		}
		colsStr := strings.Join(colList, ", ")

		colTypeMap := make(map[string]string)
		for i, c := range td.Columns {
			if i < len(td.ColumnTypes) {
				colTypeMap[c] = strings.ToUpper(td.ColumnTypes[i])
			}
		}

		// 分批插入（每次 500 行，避免超出参数限制）
		batchSize := 500
		for i := 0; i < len(td.Rows); i += batchSize {
			end := i + batchSize
			if end > len(td.Rows) {
				end = len(td.Rows)
			}
			batch := td.Rows[i:end]

			var valPlaceholders []string
			var args []any
			argIdx := 1
			for _, r := range batch {
				var phs []string
				for colIdx, val := range r {
					phs = append(phs, fmt.Sprintf("$%d", argIdx))

					colName := ""
					if colIdx < len(td.Columns) {
						colName = td.Columns[colIdx]
					}
					cType := colTypeMap[colName]

					// 专门处理 BYTEA 二进制列（包括通过类型声明识别或旧快照 fallback 识别）
					if val != nil && (cType == "BYTEA" || (td.Name == "skill_package_files" && colName == "content")) {
						if str, ok := val.(string); ok {
							if strings.HasPrefix(str, "\\x") {
								if b, err := hex.DecodeString(str[2:]); err == nil {
									args = append(args, b)
									argIdx++
									continue
								}
							}
							args = append(args, []byte(str))
							argIdx++
							continue
						}
					}

					args = append(args, val)
					argIdx++
				}
				valPlaceholders = append(valPlaceholders, fmt.Sprintf("(%s)", strings.Join(phs, ", ")))
			}

			stmt := fmt.Sprintf(`INSERT INTO %q (%s) VALUES %s;`, td.Name, colsStr, strings.Join(valPlaceholders, ", "))
			if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
				return fmt.Errorf("还原表 %s 数据失败: %w", td.Name, err)
			}
		}
	}

	// 还原序列当前值
	for _, seq := range dump.Sequences {
		q := fmt.Sprintf(`SELECT setval(%q, %d, true);`, seq.Name, seq.LastValue)
		_, _ = tx.ExecContext(ctx, q)
	}

	// 恢复外键约束
	if _, err := tx.ExecContext(ctx, "SET session_replication_role = 'origin';"); err != nil {
		return fmt.Errorf("重置 session_replication_role 失败: %w", err)
	}

	return tx.Commit()
}

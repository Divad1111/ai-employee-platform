package config

import (
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"net/url"
	"os"
	"strings"
)

func (c *Config) validateSecurity() error {
	if !strings.EqualFold(c.Env, "production") {
		return nil
	}
	for _, name := range []string{"AIE_SECRET_MASTER_KEY", "AIE_BACKUP_MASTER_KEY", "AIE_SETUP_TOKEN"} {
		value := os.Getenv(name)
		if len(value) < 32 || strings.Contains(value, "CHANGE_ME") || strings.Contains(value, "default-master") || strings.Contains(value, "dev-only") {
			return fmt.Errorf("生产环境必须配置至少 32 字符的随机 %s", name)
		}
	}
	if os.Getenv("AIE_SECRET_MASTER_KEY") == os.Getenv("AIE_BACKUP_MASTER_KEY") || os.Getenv("AIE_SETUP_TOKEN") == os.Getenv("AIE_SECRET_MASTER_KEY") || os.Getenv("AIE_SETUP_TOKEN") == os.Getenv("AIE_BACKUP_MASTER_KEY") {
		return fmt.Errorf("三组生产密钥必须独立生成，不能复用")
	}
	if os.Getenv("AIE_MASTER_KEY") != "" {
		return fmt.Errorf("请迁移旧 AIE_MASTER_KEY 为 AIE_BACKUP_MASTER_KEY，避免备份密钥优先级混淆")
	}
	if os.Getenv("AIE_DEV_SEED_ADMIN") == "1" {
		return fmt.Errorf("生产环境禁止启用演示管理员")
	}
	db, err := pgconn.ParseConfig(c.DatabaseURL)
	if err != nil || len(db.Password) < 20 || db.Password == "aie" || strings.Contains(db.Password, "CHANGE_ME") {
		return fmt.Errorf("生产数据库必须配置至少 20 字符的随机密码")
	}
	public, err := url.Parse(c.PublicServerURL)
	if err != nil || public.Scheme != "https" || public.Host == "" || public.User != nil {
		return fmt.Errorf("生产 AIE_PUBLIC_SERVER_URL 必须为可信 HTTPS 地址")
	}
	return nil
}

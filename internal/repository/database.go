package repository

import (
	"database/sql"
	"fmt"
	"time"

	"wjdr-backend-go/internal/config"

	_ "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
)

type Database struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewDatabase(cfg *config.DatabaseConfig, logger *zap.Logger) (*Database, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库连接失败: %w", err)
	}

	// 配置连接池
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Hour)

	// 测试连接
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接测试失败: %w", err)
	}

	logger.Info("✅ 数据库连接成功",
		zap.String("host", cfg.Host),
		zap.String("database", cfg.DBName),
		zap.Int("max_open_conns", cfg.MaxOpenConns),
		zap.Int("max_idle_conns", cfg.MaxIdleConns))

	return &Database{
		db:     db,
		logger: logger,
	}, nil
}

func (d *Database) GetDB() *sql.DB {
	return d.db
}

func (d *Database) Close() error {
	return d.db.Close()
}

// 健康检查
func (d *Database) Ping() error {
	return d.db.Ping()
}

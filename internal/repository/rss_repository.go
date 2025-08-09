package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"wjdr-backend-go/internal/model"

	mysql "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
)

type RSSRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewRSSRepository(db *sql.DB, logger *zap.Logger) *RSSRepository {
	return &RSSRepository{db: db, logger: logger}
}

// ensureTable 方法已移除：表由DBA手动创建，避免应用启动时自动建表

func (r *RSSRepository) IsProcessed(id string) (bool, error) {
	var exists int
	err := r.db.QueryRow(`SELECT 1 FROM processed_articles WHERE id = ? LIMIT 1`, id).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *RSSRepository) MarkProcessed(id, title, link string) error {
	_, err := r.db.Exec(`INSERT INTO processed_articles (id, title, link, processed_at) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE title=VALUES(title), link=VALUES(link)`, id, title, link, time.Now())
	return err
}

// MarkProcessedWithCodes 记录已处理文章并保存提取到的兑换码（JSON数组）
// 若表中不存在 codes_json 列，则自动回退到不含 codes 的插入，以兼容尚未升级的数据库。
func (r *RSSRepository) MarkProcessedWithCodes(id, title, link string, codes []string) error {
	// JSON 编码
	var codesJSON *string
	if len(codes) > 0 {
		b, _ := json.Marshal(codes)
		s := string(b)
		codesJSON = &s
	}

	_, err := r.db.Exec(`INSERT INTO processed_articles (id, title, link, codes_json, processed_at) VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE title=VALUES(title), link=VALUES(link), codes_json=VALUES(codes_json)`, id, title, link, codesJSON, time.Now())
	if err == nil {
		return nil
	}
	// 兼容：若列不存在或表未升级，回退到基础插入
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		if myErr.Number == 1054 || myErr.Number == 1146 { // Unknown column / Table doesn't exist
			return r.MarkProcessed(id, title, link)
		}
	}
	// 文本包含unknown column的情况
	if strings.Contains(strings.ToLower(err.Error()), "unknown column") {
		return r.MarkProcessed(id, title, link)
	}
	return err
}

// ListProcessedArticles 获取最近N条已处理文章
func (r *RSSRepository) ListProcessedArticles(limit int) ([]model.ProcessedArticle, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(`SELECT id, title, COALESCE(link, ''), codes_json, processed_at FROM processed_articles ORDER BY processed_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.ProcessedArticle
	for rows.Next() {
		var p model.ProcessedArticle
		if err := rows.Scan(&p.ID, &p.Title, &p.Link, &p.CodesJSON, &p.ProcessedAt); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, nil
}

// ListProcessedArticlesAll 获取全部已处理文章（不加LIMIT）
func (r *RSSRepository) ListProcessedArticlesAll() ([]model.ProcessedArticle, error) {
	rows, err := r.db.Query(`SELECT id, title, COALESCE(link, ''), codes_json, processed_at FROM processed_articles ORDER BY processed_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.ProcessedArticle
	for rows.Next() {
		var p model.ProcessedArticle
		if err := rows.Scan(&p.ID, &p.Title, &p.Link, &p.CodesJSON, &p.ProcessedAt); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, nil
}

package repository

import (
	"database/sql"
	"strings"
	"time"
	"wjdr-backend-go/internal/model"

	"go.uber.org/zap"
)

// OCRKeyRepository OCR Key 存储层
type OCRKeyRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewOCRKeyRepository(db *sql.DB, logger *zap.Logger) *OCRKeyRepository {
	return &OCRKeyRepository{db: db, logger: logger}
}

// ListAll 返回全部 Key（可用于管理端列表）
func (r *OCRKeyRepository) ListAll() ([]model.OCRKey, error) {
	query := `SELECT id, provider, name, api_key, secret_key, is_active, has_quota, monthly_quota, remaining_quota, weight, success_count, fail_count, last_error, last_used_at, created_at, updated_at
              FROM ocr_keys ORDER BY id ASC`
	rows, err := r.db.Query(query)
	if err != nil {
		r.logger.Error("查询 OCR Keys 失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var keys []model.OCRKey
	for rows.Next() {
		var k model.OCRKey
		var lastUsedAt sql.NullTime
		var lastError sql.NullString
		if err := rows.Scan(
			&k.ID, &k.Provider, &k.Name, &k.APIKey, &k.SecretKey, &k.IsActive, &k.HasQuota, &k.MonthlyQuota, &k.RemainingQuota, &k.Weight,
			&k.SuccessCount, &k.FailCount, &lastError, &lastUsedAt, &k.CreatedAt, &k.UpdatedAt,
		); err != nil {
			r.logger.Error("扫描 OCR Key 失败", zap.Error(err))
			return nil, err
		}
		if lastUsedAt.Valid {
			t := lastUsedAt.Time
			k.LastUsedAt = &t
		}
		if lastError.Valid {
			s := lastError.String
			k.LastError = &s
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// ListUsable 返回可参与调度的 Key
func (r *OCRKeyRepository) ListUsable() ([]model.OCRKey, error) {
	query := `SELECT id, provider, name, api_key, secret_key, is_active, has_quota, monthly_quota, remaining_quota, weight, success_count, fail_count, last_error, last_used_at, created_at, updated_at
              FROM ocr_keys WHERE is_active = TRUE AND has_quota = TRUE ORDER BY id ASC`
	rows, err := r.db.Query(query)
	if err != nil {
		r.logger.Error("查询可用 OCR Keys 失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var keys []model.OCRKey
	for rows.Next() {
		var k model.OCRKey
		var lastUsedAt sql.NullTime
		var lastError sql.NullString
		if err := rows.Scan(
			&k.ID, &k.Provider, &k.Name, &k.APIKey, &k.SecretKey, &k.IsActive, &k.HasQuota, &k.MonthlyQuota, &k.RemainingQuota, &k.Weight,
			&k.SuccessCount, &k.FailCount, &lastError, &lastUsedAt, &k.CreatedAt, &k.UpdatedAt,
		); err != nil {
			r.logger.Error("扫描可用 OCR Key 失败", zap.Error(err))
			return nil, err
		}
		if lastUsedAt.Valid {
			t := lastUsedAt.Time
			k.LastUsedAt = &t
		}
		if lastError.Valid {
			s := lastError.String
			k.LastError = &s
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// Create 新增 Key
func (r *OCRKeyRepository) Create(k model.OCRKey) (int, error) {
	query := `INSERT INTO ocr_keys (provider, name, api_key, secret_key, is_active, has_quota, monthly_quota, remaining_quota, weight)
              VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := r.db.Exec(query, k.Provider, k.Name, k.APIKey, k.SecretKey, k.IsActive, k.HasQuota, k.MonthlyQuota, k.RemainingQuota, k.Weight)
	if err != nil {
		r.logger.Error("创建 OCR Key 失败", zap.Error(err))
		return 0, err
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id64), nil
}

// Update 更新部分字段（name/is_active/has_quota/weight）
func (r *OCRKeyRepository) Update(id int, patch map[string]interface{}) error {
	// 简化：拼接动态 SQL（只允许已知字段）
	allowed := map[string]bool{
		"name": true, "is_active": true, "has_quota": true, "monthly_quota": true, "remaining_quota": true, "weight": true,
	}
	sets := make([]string, 0, len(patch))
	args := make([]interface{}, 0, len(patch)+1)
	for k, v := range patch {
		if !allowed[k] {
			continue
		}
		sets = append(sets, k+" = ?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		return nil
	}
	query := "UPDATE ocr_keys SET " + strings.Join(sets, ", ") + ", updated_at = NOW() WHERE id = ?"
	args = append(args, id)
	_, err := r.db.Exec(query, args...)
	if err != nil {
		r.logger.Error("更新 OCR Key 失败", zap.Error(err), zap.Int("id", id))
	}
	return err
}

// Delete 删除 Key
func (r *OCRKeyRepository) Delete(id int) error {
	_, err := r.db.Exec("DELETE FROM ocr_keys WHERE id = ?", id)
	if err != nil {
		r.logger.Error("删除 OCR Key 失败", zap.Error(err), zap.Int("id", id))
	}
	return err
}

// MarkQuota 设置额度状态
func (r *OCRKeyRepository) MarkQuota(id int, hasQuota bool) error {
	_, err := r.db.Exec("UPDATE ocr_keys SET has_quota = ?, updated_at = NOW() WHERE id = ?", hasQuota, id)
	if err != nil {
		r.logger.Error("更新 OCR Key 额度失败", zap.Error(err), zap.Int("id", id))
	}
	return err
}

// TouchUsage 更新使用统计
func (r *OCRKeyRepository) TouchUsage(id int, success bool, errMsg *string) error {
	if success {
		_, err := r.db.Exec("UPDATE ocr_keys SET success_count = success_count + 1, last_used_at = ?, last_error = NULL, updated_at = NOW() WHERE id = ?", time.Now(), id)
		if err != nil {
			r.logger.Error("更新 OCR Key 成功统计失败", zap.Error(err), zap.Int("id", id))
		}
		return err
	}
	_, err := r.db.Exec("UPDATE ocr_keys SET fail_count = fail_count + 1, last_used_at = ?, last_error = ?, updated_at = NOW() WHERE id = ?", time.Now(), errMsg, id)
	if err != nil {
		r.logger.Error("更新 OCR Key 失败统计失败", zap.Error(err), zap.Int("id", id))
	}
	return err
}

// DecrementQuota 扣减一次额度；若降至0则自动 has_quota=false
func (r *OCRKeyRepository) DecrementQuota(id int) error {
	query := `UPDATE ocr_keys 
              SET remaining_quota = CASE WHEN remaining_quota > 0 THEN remaining_quota - 1 ELSE 0 END,
                  has_quota = CASE WHEN remaining_quota - 1 <= 0 THEN FALSE ELSE has_quota END,
                  updated_at = NOW()
              WHERE id = ?`
	if _, err := r.db.Exec(query, id); err != nil {
		r.logger.Error("扣减OCR Key额度失败", zap.Error(err), zap.Int("id", id))
		return err
	}
	return nil
}

// ResetMonthlyQuota 将剩余额度重置为每月额度，并启用 has_quota=true（不改变 is_active）
func (r *OCRKeyRepository) ResetMonthlyQuota() error {
	query := `UPDATE ocr_keys SET remaining_quota = monthly_quota, has_quota = (monthly_quota > 0), updated_at = NOW()`
	if _, err := r.db.Exec(query); err != nil {
		r.logger.Error("重置OCR Key月额度失败", zap.Error(err))
		return err
	}
	return nil
}

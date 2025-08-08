package repository

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"
	"wjdr-backend-go/internal/model"

	"go.uber.org/zap"
)

type AdminRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewAdminRepository(db *sql.DB, logger *zap.Logger) *AdminRepository {
	return &AdminRepository{
		db:     db,
		logger: logger,
	}
}

// ValidatePassword 验证管理员密码（与Node版本对齐）
func (r *AdminRepository) ValidatePassword(password string) (bool, error) {
	// 使用SHA256加密输入的密码（与Node版本一致）
	hasher := sha256.New()
	hasher.Write([]byte(password))
	hashedPassword := fmt.Sprintf("%x", hasher.Sum(nil))

	// 查询数据库中是否存在匹配的活跃密码
	query := `SELECT id FROM admin_passwords WHERE password_hash = ? AND is_active = TRUE`

	row := r.db.QueryRow(query, hashedPassword)
	var id int
	err := row.Scan(&id)

	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil // 密码不匹配
		}
		r.logger.Error("密码验证查询失败", zap.Error(err))
		return false, err
	}

	return true, nil
}

// CreatePassword 创建新的管理员密码（与Node版本对齐）
func (r *AdminRepository) CreatePassword(password, description string) (int, error) {
	// 使用SHA256加密密码
	hasher := sha256.New()
	hasher.Write([]byte(password))
	hashedPassword := fmt.Sprintf("%x", hasher.Sum(nil))

	query := `INSERT INTO admin_passwords (password_hash, description, is_active) VALUES (?, ?, TRUE)`

	result, err := r.db.Exec(query, hashedPassword, description)
	if err != nil {
		r.logger.Error("创建管理员密码失败", zap.Error(err))
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	r.logger.Info("管理员密码创建成功", zap.Int64("id", id))
	return int(id), nil
}

// GetAllPasswords 获取所有管理员密码信息（不包含真实密码）
func (r *AdminRepository) GetAllPasswords() ([]model.AdminPassword, error) {
	query := `SELECT id, description, is_active, created_at, updated_at FROM admin_passwords ORDER BY created_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		r.logger.Error("查询管理员密码列表失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var passwords []model.AdminPassword
	for rows.Next() {
		var password model.AdminPassword
		err := rows.Scan(
			&password.ID,
			&password.Description,
			&password.IsActive,
			&password.CreatedAt,
			&password.UpdatedAt,
		)
		if err != nil {
			r.logger.Error("扫描管理员密码数据失败", zap.Error(err))
			return nil, err
		}
		passwords = append(passwords, password)
	}

	return passwords, nil
}

// DeletePassword 删除管理员密码（与Node版本对齐）
func (r *AdminRepository) DeletePassword(id int) (bool, error) {
	// 检查是否为默认密码（ID为1的不允许删除）
	if id == 1 {
		return false, fmt.Errorf("默认密码不允许删除")
	}

	query := `DELETE FROM admin_passwords WHERE id = ? AND id != 1`

	result, err := r.db.Exec(query, id)
	if err != nil {
		r.logger.Error("删除管理员密码失败", zap.Error(err), zap.Int("id", id))
		return false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected > 0, nil
}

// UpdatePasswordStatus 更新密码状态（启用/禁用）
func (r *AdminRepository) UpdatePasswordStatus(id int, isActive bool) (bool, error) {
	query := `UPDATE admin_passwords SET is_active = ? WHERE id = ?`

	result, err := r.db.Exec(query, isActive, id)
	if err != nil {
		r.logger.Error("更新密码状态失败", zap.Error(err), zap.Int("id", id))
		return false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected > 0, nil
}

// UpdateDefaultPassword 更新默认密码（与Node版本对齐）
func (r *AdminRepository) UpdateDefaultPassword(id int, newPassword, description string) (bool, error) {
	// 只允许更新ID为1的默认密码
	if id != 1 {
		return false, fmt.Errorf("只能更新默认密码")
	}

	hasher := sha256.New()
	hasher.Write([]byte(newPassword))
	hashedPassword := fmt.Sprintf("%x", hasher.Sum(nil))

	query := `UPDATE admin_passwords SET password_hash = ?, description = ? WHERE id = ?`

	result, err := r.db.Exec(query, hashedPassword, description, id)
	if err != nil {
		r.logger.Error("更新默认密码失败", zap.Error(err))
		return false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected > 0, nil
}

// === Token 管理 ===

// CreateToken 添加新token（与Node版本对齐）
func (r *AdminRepository) CreateToken(token string, expiresAt time.Time) (int, error) {
	query := `INSERT INTO admin_tokens (token, expires_at) VALUES (?, ?)`

	result, err := r.db.Exec(query, token, expiresAt)
	if err != nil {
		r.logger.Error("创建token失败", zap.Error(err))
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	return int(id), nil
}

// VerifyToken 验证token是否有效（与Node版本对齐）
func (r *AdminRepository) VerifyToken(token string) (bool, error) {
	// 使用优化的查询，利用索引
	query := `SELECT id FROM admin_tokens WHERE token = ? AND expires_at > NOW() LIMIT 1`

	row := r.db.QueryRow(query, token)
	var id int
	err := row.Scan(&id)

	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil // token无效或已过期
		}
		r.logger.Error("验证token失败", zap.Error(err))
		return false, nil // token验证失败时默认返回false，不阻断流程
	}

	return true, nil
}

// DeleteToken 删除token
func (r *AdminRepository) DeleteToken(token string) (bool, error) {
	query := `DELETE FROM admin_tokens WHERE token = ?`

	result, err := r.db.Exec(query, token)
	if err != nil {
		r.logger.Error("删除token失败", zap.Error(err))
		return false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected > 0, nil
}

// CleanExpiredTokens 清理过期token
func (r *AdminRepository) CleanExpiredTokens() (int, error) {
	query := `DELETE FROM admin_tokens WHERE expires_at <= NOW()`

	result, err := r.db.Exec(query)
	if err != nil {
		r.logger.Error("清理过期token失败", zap.Error(err))
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if rowsAffected > 0 {
		r.logger.Info("清理过期token成功", zap.Int64("count", rowsAffected))
	}

	return int(rowsAffected), nil
}

// CountValidTokens 获取有效token数量
func (r *AdminRepository) CountValidTokens() (int, error) {
	query := `SELECT COUNT(*) as count FROM admin_tokens WHERE expires_at > NOW()`

	row := r.db.QueryRow(query)
	var count int
	err := row.Scan(&count)

	if err != nil {
		r.logger.Error("获取token数量失败", zap.Error(err))
		return 0, err
	}

	return count, nil
}

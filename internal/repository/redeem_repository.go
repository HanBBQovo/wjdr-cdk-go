package repository

import (
	"database/sql"
	"fmt"
	"wjdr-backend-go/internal/model"

	"go.uber.org/zap"
)

type RedeemRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewRedeemRepository(db *sql.DB, logger *zap.Logger) *RedeemRepository {
	return &RedeemRepository{
		db:     db,
		logger: logger,
	}
}

// CreateRedeemCode 创建兑换码（与Node版本对齐）
func (r *RedeemRepository) CreateRedeemCode(code string, isLong bool) (int, error) {
	query := `INSERT INTO redeem_codes (code, status, is_long) VALUES (?, 'pending', ?)`

	result, err := r.db.Exec(query, code, isLong)
	if err != nil {
		r.logger.Error("创建兑换码失败", zap.Error(err), zap.String("code", code))
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	r.logger.Info("兑换码创建成功", zap.String("code", code), zap.Int64("id", id))
	return int(id), nil
}

// GetAllRedeemCodes 获取兑换码列表（与Node版本对齐）
func (r *RedeemRepository) GetAllRedeemCodes(limit, offset int) ([]model.RedeemCode, error) {
	query := `SELECT id, code, status, is_long, total_accounts, success_count, failed_count, created_at 
	          FROM redeem_codes ORDER BY created_at DESC LIMIT ? OFFSET ?`

	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		r.logger.Error("查询兑换码列表失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var codes []model.RedeemCode
	for rows.Next() {
		var code model.RedeemCode
		err := rows.Scan(
			&code.ID,
			&code.Code,
			&code.Status,
			&code.IsLong,
			&code.TotalAccounts,
			&code.SuccessCount,
			&code.FailedCount,
			&code.CreatedAt,
		)
		if err != nil {
			r.logger.Error("扫描兑换码数据失败", zap.Error(err))
			return nil, err
		}
		codes = append(codes, code)
	}

	return codes, nil
}

// FindRedeemCodeByID 通过ID查找兑换码
func (r *RedeemRepository) FindRedeemCodeByID(id int) (*model.RedeemCode, error) {
	query := `SELECT id, code, status, is_long, total_accounts, success_count, failed_count, created_at 
	          FROM redeem_codes WHERE id = ?`

	row := r.db.QueryRow(query, id)

	var code model.RedeemCode
	err := row.Scan(
		&code.ID,
		&code.Code,
		&code.Status,
		&code.IsLong,
		&code.TotalAccounts,
		&code.SuccessCount,
		&code.FailedCount,
		&code.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.logger.Error("查询兑换码失败", zap.Error(err), zap.Int("id", id))
		return nil, err
	}

	return &code, nil
}

// FindRedeemCodeByCode 通过兑换码字符串查找
func (r *RedeemRepository) FindRedeemCodeByCode(code string) (*model.RedeemCode, error) {
	query := `SELECT id, code, status, is_long, total_accounts, success_count, failed_count, created_at 
	          FROM redeem_codes WHERE code = ?`

	row := r.db.QueryRow(query, code)

	var redeemCode model.RedeemCode
	err := row.Scan(
		&redeemCode.ID,
		&redeemCode.Code,
		&redeemCode.Status,
		&redeemCode.IsLong,
		&redeemCode.TotalAccounts,
		&redeemCode.SuccessCount,
		&redeemCode.FailedCount,
		&redeemCode.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.logger.Error("查询兑换码失败", zap.Error(err), zap.String("code", code))
		return nil, err
	}

	return &redeemCode, nil
}

// UpdateRedeemCodeStatus 更新兑换码状态（与Node版本对齐）
func (r *RedeemRepository) UpdateRedeemCodeStatus(id int, status string, totalAccounts int) error {
	query := `UPDATE redeem_codes SET status = ?, total_accounts = ? WHERE id = ?`

	_, err := r.db.Exec(query, status, totalAccounts, id)
	if err != nil {
		r.logger.Error("更新兑换码状态失败", zap.Error(err), zap.Int("id", id))
		return err
	}

	return nil
}

// UpdateRedeemCodeStats 更新兑换码统计
func (r *RedeemRepository) UpdateRedeemCodeStats(id int, successCount, failedCount, totalAccounts int) error {
	query := `UPDATE redeem_codes SET success_count = ?, failed_count = ?, total_accounts = ? WHERE id = ?`

	_, err := r.db.Exec(query, successCount, failedCount, totalAccounts, id)
	if err != nil {
		r.logger.Error("更新兑换码统计失败", zap.Error(err), zap.Int("id", id))
		return err
	}

	return nil
}

// DeleteRedeemCode 删除兑换码（与Node版本对齐）
func (r *RedeemRepository) DeleteRedeemCode(id int) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 先删除相关的兑换日志
	deleteLogsQuery := `DELETE FROM redeem_logs WHERE redeem_code_id = ?`
	_, err = tx.Exec(deleteLogsQuery, id)
	if err != nil {
		return err
	}

	// 删除兑换码
	deleteCodeQuery := `DELETE FROM redeem_codes WHERE id = ?`
	result, err := tx.Exec(deleteCodeQuery, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("兑换码不存在")
	}

	return tx.Commit()
}

// BulkDeleteRedeemCodes 批量删除兑换码（与Node版本对齐）
func (r *RedeemRepository) BulkDeleteRedeemCodes(ids []int) (int, error) {
	if len(ids) == 0 {
		return 0, fmt.Errorf("没有指定要删除的兑换码")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// 构建占位符
	placeholders := ""
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}

	// 删除相关的兑换日志
	deleteLogsQuery := fmt.Sprintf(`DELETE FROM redeem_logs WHERE redeem_code_id IN (%s)`, placeholders)
	_, err = tx.Exec(deleteLogsQuery, args...)
	if err != nil {
		return 0, err
	}

	// 删除兑换码
	deleteCodesQuery := fmt.Sprintf(`DELETE FROM redeem_codes WHERE id IN (%s)`, placeholders)
	result, err := tx.Exec(deleteCodesQuery, args...)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	r.logger.Info("批量删除兑换码成功", zap.Int("count", int(rowsAffected)))
	return int(rowsAffected), nil
}

// GetNonLongTermCodes 获取所有非长期兑换码（定时任务用）
func (r *RedeemRepository) GetNonLongTermCodes() ([]model.RedeemCode, error) {
	query := `SELECT id, code FROM redeem_codes WHERE is_long = FALSE ORDER BY created_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []model.RedeemCode
	for rows.Next() {
		var code model.RedeemCode
		err := rows.Scan(&code.ID, &code.Code)
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}

	return codes, nil
}

// GetCompletedRedeemCodes 获取所有已完成的兑换码（定时任务用）
func (r *RedeemRepository) GetCompletedRedeemCodes() ([]model.RedeemCode, error) {
	query := `SELECT id, code FROM redeem_codes WHERE status = 'completed' ORDER BY created_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []model.RedeemCode
	for rows.Next() {
		var code model.RedeemCode
		err := rows.Scan(&code.ID, &code.Code)
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}

	return codes, nil
}

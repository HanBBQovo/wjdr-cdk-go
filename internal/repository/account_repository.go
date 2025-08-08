package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"wjdr-backend-go/internal/model"

	mysql "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
)

type AccountRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewAccountRepository(db *sql.DB, logger *zap.Logger) *AccountRepository {
	return &AccountRepository{
		db:     db,
		logger: logger,
	}
}

// Create 创建新账号（与Node版本对齐）
func (r *AccountRepository) Create(fid, nickname string, avatarImage *string, stoveLv *int, stoveLvContent *string) (int, error) {
	query := `INSERT INTO game_accounts (fid, nickname, avatar_image, stove_lv, stove_lv_content, is_verified) 
			  VALUES (?, ?, ?, ?, ?, true)`

	result, err := r.db.Exec(query, fid, nickname, avatarImage, stoveLv, stoveLvContent)
	if err != nil {
		r.logger.Error("创建账号失败", zap.Error(err), zap.String("fid", fid))
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	r.logger.Info("账号创建成功", zap.String("fid", fid), zap.String("nickname", nickname))
	return int(id), nil
}

// GetAll 获取所有账号（与Node版本对齐）
func (r *AccountRepository) GetAll() ([]model.Account, error) {
	query := `SELECT id, fid, nickname, avatar_image, stove_lv, stove_lv_content, 
			  is_active, is_verified, last_login_check, created_at 
			  FROM game_accounts ORDER BY created_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		r.logger.Error("查询账号列表失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var accounts []model.Account
	for rows.Next() {
		var account model.Account
		err := rows.Scan(
			&account.ID,
			&account.FID,
			&account.Nickname,
			&account.AvatarImage,
			&account.StoveLv,
			&account.StoveLvContent,
			&account.IsActive,
			&account.IsVerified,
			&account.LastLoginCheck,
			&account.CreatedAt,
		)
		if err != nil {
			r.logger.Error("扫描账号数据失败", zap.Error(err))
			return nil, err
		}
		accounts = append(accounts, account)
	}

	return accounts, nil
}

// GetActive 获取活跃账号
func (r *AccountRepository) GetActive() ([]model.Account, error) {
	query := `SELECT id, fid, nickname, avatar_image, stove_lv, stove_lv_content, 
			  is_active, is_verified, last_login_check, created_at 
			  FROM game_accounts WHERE is_active = true ORDER BY created_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		r.logger.Error("查询活跃账号失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var accounts []model.Account
	for rows.Next() {
		var account model.Account
		err := rows.Scan(
			&account.ID,
			&account.FID,
			&account.Nickname,
			&account.AvatarImage,
			&account.StoveLv,
			&account.StoveLvContent,
			&account.IsActive,
			&account.IsVerified,
			&account.LastLoginCheck,
			&account.CreatedAt,
		)
		if err != nil {
			r.logger.Error("扫描活跃账号数据失败", zap.Error(err))
			return nil, err
		}
		accounts = append(accounts, account)
	}

	return accounts, nil
}

// FindByFID 通过FID查找账号
func (r *AccountRepository) FindByFID(fid string) (*model.Account, error) {
	query := `SELECT id, fid, nickname, avatar_image, stove_lv, stove_lv_content, 
			  is_active, is_verified, last_login_check, created_at 
			  FROM game_accounts WHERE fid = ?`

	row := r.db.QueryRow(query, fid)

	var account model.Account
	err := row.Scan(
		&account.ID,
		&account.FID,
		&account.Nickname,
		&account.AvatarImage,
		&account.StoveLv,
		&account.StoveLvContent,
		&account.IsActive,
		&account.IsVerified,
		&account.LastLoginCheck,
		&account.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // 账号不存在
		}
		r.logger.Error("查询账号失败", zap.Error(err), zap.String("fid", fid))
		return nil, err
	}

	return &account, nil
}

// UpdateVerifyStatus 更新验证状态（与Node版本对齐）
func (r *AccountRepository) UpdateVerifyStatus(id int, success bool) error {
	query := `UPDATE game_accounts SET is_verified = ?, last_login_check = NOW() WHERE id = ?`

	_, err := r.db.Exec(query, success, id)
	if err != nil {
		r.logger.Error("更新账号验证状态失败", zap.Error(err), zap.Int("id", id))
		return err
	}

	return nil
}

// Delete 删除账号（与Node版本对齐）
func (r *AccountRepository) Delete(id int) error {
	const maxRetries = 3

	isDeadlock := func(err error) bool {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) {
			// 1213: Deadlock, 1205: Lock wait timeout
			return mysqlErr.Number == 1213 || mysqlErr.Number == 1205
		}
		if err != nil {
			msg := strings.ToLower(err.Error())
			return strings.Contains(msg, "deadlock") || strings.Contains(msg, "lock wait timeout")
		}
		return false
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// 开始事务
		tx, err := r.db.Begin()
		if err != nil {
			return err
		}

		// 读取受影响的兑换码（使用 ORDER BY 确保一致顺序，减少死锁）
		affectedCodesQuery := `
            SELECT DISTINCT redeem_code_id 
            FROM redeem_logs 
            WHERE game_account_id = ?
            ORDER BY redeem_code_id ASC`
		rows, err := tx.Query(affectedCodesQuery, id)
		if err != nil {
			tx.Rollback()
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("删除账号-读取受影响兑换码发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return err
		}

		var affectedRedeemCodes []int
		for rows.Next() {
			var redeemCodeID int
			if err := rows.Scan(&redeemCodeID); err != nil {
				rows.Close()
				tx.Rollback()
				if isDeadlock(err) && attempt < maxRetries {
					r.logger.Warn("删除账号-扫描受影响兑换码发生死锁，重试", zap.Int("attempt", attempt))
					time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
					continue
				}
				return err
			}
			affectedRedeemCodes = append(affectedRedeemCodes, redeemCodeID)
		}
		rows.Close()

		// 先显式删除该账号的兑换日志，避免依赖外键级联未配置导致统计不变
		if _, err = tx.Exec(`DELETE FROM redeem_logs WHERE game_account_id = ?`, id); err != nil {
			tx.Rollback()
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("删除账号-删除兑换日志发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return err
		}

		// 再删除账号
		if _, err = tx.Exec(`DELETE FROM game_accounts WHERE id = ?`, id); err != nil {
			tx.Rollback()
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("删除账号-执行删除发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return err
		}

		// 依次更新统计，若其中出现死锁，回滚并重试整个事务
		shouldRetry := false
		for _, redeemCodeID := range affectedRedeemCodes {
			if err := r.updateRedeemCodeStats(tx, redeemCodeID); err != nil {
				if isDeadlock(err) && attempt < maxRetries {
					r.logger.Warn("删除账号-更新统计发生死锁，重试", zap.Int("attempt", attempt), zap.Int("redeem_code_id", redeemCodeID))
					shouldRetry = true
					break
				}
				tx.Rollback()
				return err
			}
		}

		if shouldRetry {
			tx.Rollback()
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
			continue
		}

		if err := tx.Commit(); err != nil {
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("删除账号-提交事务发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return err
		}

		// 成功
		return nil
	}

	return fmt.Errorf("删除账号在重试 %d 次后仍失败（死锁）", maxRetries)
}

// BulkDelete 批量删除账号（带统计更新）
func (r *AccountRepository) BulkDelete(ids []int) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	// 事务：删除所有账号对应的redeem_logs，再删除账号，最后批量更新受影响兑换码统计
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}

	// 收集受影响兑换码
	affectedMap := make(map[int]struct{})
	for _, id := range ids {
		rows, err := tx.Query(`SELECT DISTINCT redeem_code_id FROM redeem_logs WHERE game_account_id = ?`, id)
		if err != nil {
			tx.Rollback()
			return 0, err
		}
		for rows.Next() {
			var codeID int
			if err := rows.Scan(&codeID); err != nil {
				rows.Close()
				tx.Rollback()
				return 0, err
			}
			affectedMap[codeID] = struct{}{}
		}
		rows.Close()

		if _, err := tx.Exec(`DELETE FROM redeem_logs WHERE game_account_id = ?`, id); err != nil {
			tx.Rollback()
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM game_accounts WHERE id = ?`, id); err != nil {
			tx.Rollback()
			return 0, err
		}
	}

	// 更新受影响兑换码统计
	for codeID := range affectedMap {
		if err := r.updateRedeemCodeStats(tx, codeID); err != nil {
			tx.Rollback()
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return len(ids), nil
}

// updateRedeemCodeStats 重新计算兑换码统计数据（与Node版本对齐）
func (r *AccountRepository) updateRedeemCodeStats(tx *sql.Tx, redeemCodeID int) error {
	statsQuery := `
		SELECT 
			COUNT(*) as total_accounts,
            COALESCE(SUM(CASE WHEN result = 'success' THEN 1 ELSE 0 END), 0) as success_count,
            COALESCE(SUM(CASE WHEN result = 'failed' THEN 1 ELSE 0 END), 0) as failed_count
		FROM redeem_logs 
		WHERE redeem_code_id = ?
	`

	row := tx.QueryRow(statsQuery, redeemCodeID)

	var totalAccounts, successCount, failedCount int
	err := row.Scan(&totalAccounts, &successCount, &failedCount)
	if err != nil {
		return err
	}

	// 更新兑换码表的统计数据
	updateQuery := `
		UPDATE redeem_codes 
		SET 
			total_accounts = ?,
			success_count = ?,
			failed_count = ?
		WHERE id = ?
	`

	_, err = tx.Exec(updateQuery, totalAccounts, successCount, failedCount, redeemCodeID)
	if err != nil {
		return err
	}

	// 日志降噪：单个兑换码统计更新改为调试级别
	r.logger.Debug("已更新兑换码统计",
		zap.Int("redeem_code_id", redeemCodeID),
		zap.Int("total", totalAccounts),
		zap.Int("success", successCount),
		zap.Int("failed", failedCount))

	return nil
}

// FixAllRedeemCodeStats 修复所有兑换码的统计数据（与Node版本对齐）
func (r *AccountRepository) FixAllRedeemCodeStats() (int, error) {
	// 获取所有兑换码
	rows, err := r.db.Query(`SELECT id FROM redeem_codes ORDER BY id ASC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var redeemCodeIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		redeemCodeIDs = append(redeemCodeIDs, id)
	}

	// 为每个兑换码修复统计
	fixedCount := 0
	for _, id := range redeemCodeIDs {
		tx, err := r.db.Begin()
		if err != nil {
			return fixedCount, err
		}

		if err := r.updateRedeemCodeStats(tx, id); err != nil {
			tx.Rollback()
			return fixedCount, err
		}

		if err := tx.Commit(); err != nil {
			return fixedCount, err
		}

		fixedCount++
	}

	// 日志降噪：仓储层仅输出调试级别，服务层输出摘要Info
	r.logger.Debug("修复兑换码统计完成", zap.Int("fixed_count", fixedCount))
	return fixedCount, nil
}

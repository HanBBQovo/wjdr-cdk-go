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

// UpdateFromLoginData 根据登录结果更新账号的资料与验证状态
func (r *AccountRepository) UpdateFromLoginData(id int, nickname string, avatarImage *string, stoveLv *int, stoveLvContent *string, isVerified bool) error {
	query := `UPDATE game_accounts 
              SET nickname = ?, avatar_image = ?, stove_lv = ?, stove_lv_content = ?, is_verified = ?, last_login_check = NOW()
              WHERE id = ?`
	var avatarAny interface{}
	if avatarImage != nil && *avatarImage != "" {
		avatarAny = *avatarImage
	} else {
		avatarAny = nil
	}
	var stoveAny interface{}
	if stoveLv != nil {
		stoveAny = *stoveLv
	} else {
		stoveAny = nil
	}
	var stoveContentAny interface{}
	if stoveLvContent != nil && *stoveLvContent != "" {
		stoveContentAny = *stoveLvContent
	} else {
		stoveContentAny = nil
	}
	if _, err := r.db.Exec(query, nickname, avatarAny, stoveAny, stoveContentAny, isVerified, id); err != nil {
		r.logger.Error("更新账号登录资料失败", zap.Error(err), zap.Int("id", id))
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

		// 聚合读取本账号将影响的各兑换码统计增量（减少全表扫描与多次统计）
		aggRows, err := tx.Query(`
            SELECT redeem_code_id, result, COUNT(*) 
            FROM redeem_logs 
            WHERE game_account_id = ? 
            GROUP BY redeem_code_id, result 
            ORDER BY redeem_code_id ASC`, id)
		if err != nil {
			tx.Rollback()
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("删除账号-读取聚合统计发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return err
		}

		// 统计每个兑换码需要扣减的 success/failed/total
		type counters struct{ success, failed, total int }
		delMap := make(map[int]*counters)
		for aggRows.Next() {
			var codeID int
			var result string
			var cnt int
			if err := aggRows.Scan(&codeID, &result, &cnt); err != nil {
				aggRows.Close()
				tx.Rollback()
				if isDeadlock(err) && attempt < maxRetries {
					r.logger.Warn("删除账号-扫描聚合统计发生死锁，重试", zap.Int("attempt", attempt))
					time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
					continue
				}
				return err
			}
			c, ok := delMap[codeID]
			if !ok {
				c = &counters{}
				delMap[codeID] = c
			}
			if result == "success" {
				c.success += cnt
			} else if result == "failed" {
				c.failed += cnt
			}
			c.total += cnt
		}
		aggRows.Close()

		// 删除兑换日志与账号
		if _, err = tx.Exec(`DELETE FROM redeem_logs WHERE game_account_id = ?`, id); err != nil {
			tx.Rollback()
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("删除账号-删除兑换日志发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return err
		}
		if _, err = tx.Exec(`DELETE FROM game_accounts WHERE id = ?`, id); err != nil {
			tx.Rollback()
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("删除账号-执行删除发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return err
		}

		// 直接按增量扣减各兑换码统计（使用 GREATEST 防止负值）
		for codeID, c := range delMap {
			if _, err := tx.Exec(`
                UPDATE redeem_codes 
                SET 
                    total_accounts = GREATEST(0, total_accounts - ?),
                    success_count  = GREATEST(0, success_count  - ?),
                    failed_count   = GREATEST(0, failed_count   - ?)
                WHERE id = ?`, c.total, c.success, c.failed, codeID); err != nil {
				tx.Rollback()
				if isDeadlock(err) && attempt < maxRetries {
					r.logger.Warn("删除账号-增量更新统计发生死锁，重试", zap.Int("attempt", attempt), zap.Int("redeem_code_id", codeID))
					time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
					continue
				}
				return err
			}
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

	// 与单个删除保持一致：支持死锁重试
	const maxRetries = 3
	isDeadlock := func(err error) bool {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) {
			return mysqlErr.Number == 1213 || mysqlErr.Number == 1205
		}
		if err != nil {
			msg := strings.ToLower(err.Error())
			return strings.Contains(msg, "deadlock") || strings.Contains(msg, "lock wait timeout")
		}
		return false
	}

	// 构造 IN 占位符
	placeholders := make([]string, 0, len(ids))
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	inClause := strings.Join(placeholders, ",")

	for attempt := 1; attempt <= maxRetries; attempt++ {
		tx, err := r.db.Begin()
		if err != nil {
			return 0, err
		}

		// 聚合读取将要删除的账号在各兑换码上的计数
		aggQuery := `SELECT redeem_code_id, result, COUNT(*) FROM redeem_logs WHERE game_account_id IN (` + inClause + `) GROUP BY redeem_code_id, result`
		aggRows, err := tx.Query(aggQuery, args...)
		if err != nil {
			tx.Rollback()
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("批量删除-读取聚合统计发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return 0, err
		}

		type counters struct{ success, failed, total int }
		delMap := make(map[int]*counters)
		for aggRows.Next() {
			var codeID int
			var result string
			var cnt int
			if err := aggRows.Scan(&codeID, &result, &cnt); err != nil {
				aggRows.Close()
				tx.Rollback()
				if isDeadlock(err) && attempt < maxRetries {
					r.logger.Warn("批量删除-扫描聚合统计发生死锁，重试", zap.Int("attempt", attempt))
					time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
					continue
				}
				return 0, err
			}
			c, ok := delMap[codeID]
			if !ok {
				c = &counters{}
				delMap[codeID] = c
			}
			if result == "success" {
				c.success += cnt
			} else if result == "failed" {
				c.failed += cnt
			}
			c.total += cnt
		}
		aggRows.Close()

		// 删除日志与账号（IN 批量）
		if _, err := tx.Exec(`DELETE FROM redeem_logs WHERE game_account_id IN (`+inClause+`)`, args...); err != nil {
			tx.Rollback()
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("批量删除-删除兑换日志发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM game_accounts WHERE id IN (`+inClause+`)`, args...); err != nil {
			tx.Rollback()
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("批量删除-删除账号发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return 0, err
		}

		// 增量扣减所有受影响兑换码统计
		for codeID, c := range delMap {
			if _, err := tx.Exec(`
                UPDATE redeem_codes 
                SET 
                    total_accounts = GREATEST(0, total_accounts - ?),
                    success_count  = GREATEST(0, success_count  - ?),
                    failed_count   = GREATEST(0, failed_count   - ?)
                WHERE id = ?`, c.total, c.success, c.failed, codeID); err != nil {
				tx.Rollback()
				if isDeadlock(err) && attempt < maxRetries {
					r.logger.Warn("批量删除-增量更新统计发生死锁，重试", zap.Int("attempt", attempt), zap.Int("redeem_code_id", codeID))
					time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
					continue
				}
				return 0, err
			}
		}

		if err := tx.Commit(); err != nil {
			if isDeadlock(err) && attempt < maxRetries {
				r.logger.Warn("批量删除-提交事务发生死锁，重试", zap.Int("attempt", attempt))
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
				continue
			}
			return 0, err
		}

		return len(ids), nil
	}

	return 0, fmt.Errorf("批量删除在重试 %d 次后仍失败（死锁）", maxRetries)
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

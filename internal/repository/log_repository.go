package repository

import (
	"database/sql"
	"wjdr-backend-go/internal/model"

	"go.uber.org/zap"
)

type LogRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewLogRepository(db *sql.DB, logger *zap.Logger) *LogRepository {
	return &LogRepository{
		db:     db,
		logger: logger,
	}
}

// CreateRedeemLog 创建兑换记录（与Node版本对齐）
func (r *LogRepository) CreateRedeemLog(
	redeemCodeID int,
	gameAccountID int,
	fid string,
	code string,
	result string,
	errorMessage *string,
	successMessage *string,
	captchaRecognized *string,
	processingTime *int,
	errCode *int,
) (int, error) {
	query := `
		INSERT INTO redeem_logs 
		(redeem_code_id, game_account_id, fid, code, result, error_message, success_message, captcha_recognized, processing_time, err_code) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result_db, err := r.db.Exec(query,
		redeemCodeID,
		gameAccountID,
		fid,
		code,
		result,
		errorMessage,
		successMessage,
		captchaRecognized,
		processingTime,
		errCode,
	)
	if err != nil {
		r.logger.Error("创建兑换日志失败",
			zap.Error(err),
			zap.Int("redeem_code_id", redeemCodeID),
			zap.Int("account_id", gameAccountID),
			zap.String("result", result))
		return 0, err
	}

	id, err := result_db.LastInsertId()
	if err != nil {
		return 0, err
	}

	return int(id), nil
}

// ReplaceRedeemLog 替换式写入兑换记录：同一兑换码+账号只保留一条记录
// 实现方式：先删除旧记录，再插入新记录，保证最终只有一次结果（便于避免冷却重试期间的重复日志）
func (r *LogRepository) ReplaceRedeemLog(
	redeemCodeID int,
	gameAccountID int,
	fid string,
	code string,
	result string,
	errorMessage *string,
	successMessage *string,
	captchaRecognized *string,
	processingTime *int,
	errCode *int,
) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// 删除旧记录
	delQuery := `DELETE FROM redeem_logs WHERE redeem_code_id = ? AND game_account_id = ?`
	if _, err := tx.Exec(delQuery, redeemCodeID, gameAccountID); err != nil {
		r.logger.Error("删除旧兑换日志失败", zap.Error(err))
		return 0, err
	}

	// 插入新记录
	insQuery := `
        INSERT INTO redeem_logs 
        (redeem_code_id, game_account_id, fid, code, result, error_message, success_message, captcha_recognized, processing_time, err_code) 
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `
	res, err := tx.Exec(insQuery,
		redeemCodeID,
		gameAccountID,
		fid,
		code,
		result,
		errorMessage,
		successMessage,
		captchaRecognized,
		processingTime,
		errCode,
	)
	if err != nil {
		r.logger.Error("写入兑换日志失败", zap.Error(err))
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(id), nil
}

// GetLogsByRedeemCodeID 获取兑换码的所有日志（与Node版本对齐）
func (r *LogRepository) GetLogsByRedeemCodeID(redeemCodeID int) ([]model.RedeemLog, error) {
	query := `
		SELECT * FROM redeem_logs 
		WHERE redeem_code_id = ? 
		ORDER BY redeemed_at DESC
	`

	rows, err := r.db.Query(query, redeemCodeID)
	if err != nil {
		r.logger.Error("查询兑换日志失败", zap.Error(err), zap.Int("redeem_code_id", redeemCodeID))
		return nil, err
	}
	defer rows.Close()

	var logs []model.RedeemLog
	for rows.Next() {
		var log model.RedeemLog
		err := rows.Scan(
			&log.ID,
			&log.RedeemCodeID,
			&log.GameAccountID,
			&log.FID,
			&log.Code,
			&log.Result,
			&log.ErrorMessage,
			&log.SuccessMessage,
			&log.CaptchaRecognized,
			&log.ProcessingTime,
			&log.ErrCode,
			&log.RedeemedAt,
		)
		if err != nil {
			r.logger.Error("扫描兑换日志失败", zap.Error(err))
			return nil, err
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// GetLogsByAccountID 获取账号的兑换历史（与Node版本对齐）
func (r *LogRepository) GetLogsByAccountID(accountID int) ([]model.RedeemLog, error) {
	query := `
		SELECT * FROM redeem_logs 
		WHERE game_account_id = ? 
		ORDER BY redeemed_at DESC
	`

	rows, err := r.db.Query(query, accountID)
	if err != nil {
		r.logger.Error("查询账号兑换历史失败", zap.Error(err), zap.Int("account_id", accountID))
		return nil, err
	}
	defer rows.Close()

	var logs []model.RedeemLog
	for rows.Next() {
		var log model.RedeemLog
		err := rows.Scan(
			&log.ID,
			&log.RedeemCodeID,
			&log.GameAccountID,
			&log.FID,
			&log.Code,
			&log.Result,
			&log.ErrorMessage,
			&log.SuccessMessage,
			&log.CaptchaRecognized,
			&log.ProcessingTime,
			&log.ErrCode,
			&log.RedeemedAt,
		)
		if err != nil {
			r.logger.Error("扫描账号兑换历史失败", zap.Error(err))
			return nil, err
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// GetRecentLogs 获取最近的兑换记录（与Node版本对齐）
func (r *LogRepository) GetRecentLogs(limit int) ([]model.RedeemLog, error) {
	query := `SELECT id, redeem_code_id, game_account_id, fid, code, result, error_message, success_message, 
	          captcha_recognized, processing_time, err_code, redeemed_at 
	          FROM redeem_logs ORDER BY redeemed_at DESC LIMIT ?`

	rows, err := r.db.Query(query, limit)
	if err != nil {
		r.logger.Error("查询最近兑换记录失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var logs []model.RedeemLog
	for rows.Next() {
		var log model.RedeemLog
		err := rows.Scan(
			&log.ID,
			&log.RedeemCodeID,
			&log.GameAccountID,
			&log.FID,
			&log.Code,
			&log.Result,
			&log.ErrorMessage,
			&log.SuccessMessage,
			&log.CaptchaRecognized,
			&log.ProcessingTime,
			&log.ErrCode,
			&log.RedeemedAt,
		)
		if err != nil {
			r.logger.Error("扫描最近兑换记录失败", zap.Error(err))
			return nil, err
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// GetRecentLogsFiltered 获取最近的兑换记录并按结果过滤（已废弃，保留以兼容旧代码）
func (r *LogRepository) GetRecentLogsFiltered(limit int, result string) ([]model.RedeemLog, error) {
	var (
		query string
		rows  *sql.Rows
		err   error
	)

	if result == "" {
		// 无过滤时与 GetRecentLogs 一致
		query = `SELECT id, redeem_code_id, game_account_id, fid, code, result, error_message, success_message,
                 captcha_recognized, processing_time, err_code, redeemed_at
                 FROM redeem_logs ORDER BY redeemed_at DESC LIMIT ?`
		rows, err = r.db.Query(query, limit)
	} else {
		query = `SELECT id, redeem_code_id, game_account_id, fid, code, result, error_message, success_message,
                 captcha_recognized, processing_time, err_code, redeemed_at
                 FROM redeem_logs WHERE result = ? ORDER BY redeemed_at DESC LIMIT ?`
		rows, err = r.db.Query(query, result, limit)
	}
	if err != nil {
		r.logger.Error("查询最近兑换记录失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var logs []model.RedeemLog
	for rows.Next() {
		var log model.RedeemLog
		err := rows.Scan(
			&log.ID,
			&log.RedeemCodeID,
			&log.GameAccountID,
			&log.FID,
			&log.Code,
			&log.Result,
			&log.ErrorMessage,
			&log.SuccessMessage,
			&log.CaptchaRecognized,
			&log.ProcessingTime,
			&log.ErrCode,
			&log.RedeemedAt,
		)
		if err != nil {
			r.logger.Error("扫描最近兑换记录失败", zap.Error(err))
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, nil
}

// GetAllLogs 获取全部兑换记录（去除分页）
func (r *LogRepository) GetAllLogs() ([]model.RedeemLog, error) {
	query := `SELECT id, redeem_code_id, game_account_id, fid, code, result, error_message, success_message,
              captcha_recognized, processing_time, err_code, redeemed_at
              FROM redeem_logs ORDER BY redeemed_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		r.logger.Error("查询兑换记录失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var logs []model.RedeemLog
	for rows.Next() {
		var log model.RedeemLog
		err := rows.Scan(
			&log.ID,
			&log.RedeemCodeID,
			&log.GameAccountID,
			&log.FID,
			&log.Code,
			&log.Result,
			&log.ErrorMessage,
			&log.SuccessMessage,
			&log.CaptchaRecognized,
			&log.ProcessingTime,
			&log.ErrCode,
			&log.RedeemedAt,
		)
		if err != nil {
			r.logger.Error("扫描兑换记录失败", zap.Error(err))
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, nil
}

// GetAllLogsFiltered 获取全部兑换记录并按结果过滤（去除分页）
func (r *LogRepository) GetAllLogsFiltered(result string) ([]model.RedeemLog, error) {
	var (
		query string
		rows  *sql.Rows
		err   error
	)

	if result == "" {
		query = `SELECT id, redeem_code_id, game_account_id, fid, code, result, error_message, success_message,
                 captcha_recognized, processing_time, err_code, redeemed_at
                 FROM redeem_logs ORDER BY redeemed_at DESC`
		rows, err = r.db.Query(query)
	} else {
		query = `SELECT id, redeem_code_id, game_account_id, fid, code, result, error_message, success_message,
                 captcha_recognized, processing_time, err_code, redeemed_at
                 FROM redeem_logs WHERE result = ? ORDER BY redeemed_at DESC`
		rows, err = r.db.Query(query, result)
	}
	if err != nil {
		r.logger.Error("查询兑换记录失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var logs []model.RedeemLog
	for rows.Next() {
		var log model.RedeemLog
		err := rows.Scan(
			&log.ID,
			&log.RedeemCodeID,
			&log.GameAccountID,
			&log.FID,
			&log.Code,
			&log.Result,
			&log.ErrorMessage,
			&log.SuccessMessage,
			&log.CaptchaRecognized,
			&log.ProcessingTime,
			&log.ErrCode,
			&log.RedeemedAt,
		)
		if err != nil {
			r.logger.Error("扫描兑换记录失败", zap.Error(err))
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, nil
}

// DeleteLogsByRedeemCodeID 根据兑换码ID删除所有相关日志
func (r *LogRepository) DeleteLogsByRedeemCodeID(redeemCodeID int) (int, error) {
	query := `DELETE FROM redeem_logs WHERE redeem_code_id = ?`

	result, err := r.db.Exec(query, redeemCodeID)
	if err != nil {
		r.logger.Error("删除兑换日志失败", zap.Error(err), zap.Int("redeem_code_id", redeemCodeID))
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return int(rowsAffected), nil
}

// GetParticipatedAccountIDs 获取已参与过该兑换码的账号ID列表（用于补充兑换）
func (r *LogRepository) GetParticipatedAccountIDs(redeemCodeID int) ([]int, error) {
	query := `SELECT DISTINCT game_account_id FROM redeem_logs WHERE redeem_code_id = ?`

	rows, err := r.db.Query(query, redeemCodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accountIDs []int
	for rows.Next() {
		var accountID int
		if err := rows.Scan(&accountID); err != nil {
			return nil, err
		}
		accountIDs = append(accountIDs, accountID)
	}

	return accountIDs, nil
}

// GetLogStats 获取兑换码的统计信息（用于更新兑换码统计）
func (r *LogRepository) GetLogStats(redeemCodeID int) (total, success, failed int, err error) {
	query := `
		SELECT 
            COUNT(*) as total_accounts,
            COALESCE(SUM(CASE WHEN result = 'success' THEN 1 ELSE 0 END), 0) as success_count,
            COALESCE(SUM(CASE WHEN result = 'failed' THEN 1 ELSE 0 END), 0) as failed_count
		FROM redeem_logs 
		WHERE redeem_code_id = ?
	`

	row := r.db.QueryRow(query, redeemCodeID)
	err = row.Scan(&total, &success, &failed)
	if err != nil {
		r.logger.Error("获取兑换统计失败", zap.Error(err), zap.Int("redeem_code_id", redeemCodeID))
		return 0, 0, 0, err
	}

	return total, success, failed, nil
}

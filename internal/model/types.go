package model

import (
	"time"
)

// Account 游戏账号模型
type Account struct {
	ID             int        `json:"id" db:"id"`
	FID            string     `json:"fid" db:"fid"`
	Nickname       string     `json:"nickname" db:"nickname"`
	AvatarImage    *string    `json:"avatar_image" db:"avatar_image"`
	StoveLv        *int       `json:"stove_lv" db:"stove_lv"`
	StoveLvContent *string    `json:"stove_lv_content" db:"stove_lv_content"`
	IsActive       bool       `json:"is_active" db:"is_active"`
	IsVerified     bool       `json:"is_verified" db:"is_verified"`
	LastLoginCheck *time.Time `json:"last_login_check" db:"last_login_check"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// RedeemCode 兑换码模型
type RedeemCode struct {
	ID            int       `json:"id" db:"id"`
	Code          string    `json:"code" db:"code"`
	Status        string    `json:"status" db:"status"`
	IsLong        bool      `json:"is_long" db:"is_long"`
	TotalAccounts int       `json:"total_accounts" db:"total_accounts"`
	SuccessCount  int       `json:"success_count" db:"success_count"`
	FailedCount   int       `json:"failed_count" db:"failed_count"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// RedeemLog 兑换日志模型
type RedeemLog struct {
	ID                int       `json:"id" db:"id"`
	RedeemCodeID      int       `json:"redeem_code_id" db:"redeem_code_id"`
	GameAccountID     int       `json:"game_account_id" db:"game_account_id"`
	FID               string    `json:"fid" db:"fid"`
	Nickname          *string   `json:"nickname,omitempty" db:"nickname"`
	Code              string    `json:"code" db:"code"`
	Result            string    `json:"result" db:"result"`
	ErrorMessage      *string   `json:"error_message" db:"error_message"`
	SuccessMessage    *string   `json:"success_message" db:"success_message"`
	CaptchaRecognized *string   `json:"captcha_recognized" db:"captcha_recognized"`
	ProcessingTime    *int      `json:"processing_time" db:"processing_time"`
	ErrCode           *int      `json:"err_code" db:"err_code"`
	RedeemedAt        time.Time `json:"redeemed_at" db:"redeemed_at"`
}

// AdminPassword 管理员密码模型
type AdminPassword struct {
	ID           int       `json:"id" db:"id"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Description  string    `json:"description" db:"description"`
	IsActive     bool      `json:"is_active" db:"is_active"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// AdminToken 管理员Token模型
type AdminToken struct {
	ID        int       `json:"id" db:"id"`
	Token     string    `json:"token" db:"token"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Job 异步任务模型
type Job struct {
	ID           int64     `json:"id" db:"id"`
	Type         string    `json:"type" db:"type"`
	Payload      string    `json:"payload" db:"payload"`
	Status       string    `json:"status" db:"status"`
	Retries      int       `json:"retries" db:"retries"`
	MaxRetries   int       `json:"max_retries" db:"max_retries"`
	NextRunAt    time.Time `json:"next_run_at" db:"next_run_at"`
	ErrorMessage *string   `json:"error_message" db:"error_message"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// ProcessedArticle 已处理的RSS文章记录
type ProcessedArticle struct {
	ID          string    `json:"id" db:"id"`
	Title       string    `json:"title" db:"title"`
	Link        string    `json:"link" db:"link"`
	CodesJSON   *string   `json:"codes_json,omitempty" db:"codes_json"`
	ProcessedAt time.Time `json:"processed_at" db:"processed_at"`
}

// JobPayload 任务载荷结构
type JobPayload struct {
	RedeemCodeID  int   `json:"redeem_code_id"`
	AccountIDs    []int `json:"account_ids,omitempty"`
	IsRetry       bool  `json:"is_retry,omitempty"`
	SkipAccountID *int  `json:"skip_account_id,omitempty"`
}

// OCRKey OCR Key 管理模型
type OCRKey struct {
	ID             int        `json:"id" db:"id"`
	Provider       string     `json:"provider" db:"provider"`
	Name           string     `json:"name" db:"name"`
	APIKey         string     `json:"api_key" db:"api_key"`
	SecretKey      string     `json:"secret_key" db:"secret_key"`
	IsActive       bool       `json:"is_active" db:"is_active"`
	HasQuota       bool       `json:"has_quota" db:"has_quota"`
	MonthlyQuota   int        `json:"monthly_quota" db:"monthly_quota"`
	RemainingQuota int        `json:"remaining_quota" db:"remaining_quota"`
	Weight         int        `json:"weight" db:"weight"`
	SuccessCount   int        `json:"success_count" db:"success_count"`
	FailCount      int        `json:"fail_count" db:"fail_count"`
	LastError      *string    `json:"last_error,omitempty" db:"last_error"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

// API响应结构
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

// 以前的分页响应与分页结构已移除（不再分页）

// 账号验证响应
type AccountVerifyResponse struct {
	Success bool `json:"success"`
	Data    *struct {
		FID                 string `json:"fid"`
		Nickname            string `json:"nickname"`
		AvatarImage         string `json:"avatar_image"`
		StoveLv             int    `json:"stove_lv"`
		StoveLvContent      string `json:"stove_lv_content"`
		Kid                 string `json:"kid"`
		TotalRechargeAmount int    `json:"total_recharge_amount"`
		Timestamp           string `json:"timestamp"`
	} `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
	ErrCode *int   `json:"errCode,omitempty"`
}

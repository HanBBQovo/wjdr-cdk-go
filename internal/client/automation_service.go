package client

import (
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"
)

// AutomationService 游戏自动化服务（复刻Node版本的完整兑换流程）
type AutomationService struct {
	gameClient *GameClient
	ocrClient  *OCRClient
	logger     *zap.Logger
}

// 全局账号兑换闸门：保证任意时刻仅有一个账号执行完整兑换流程
var accountRedeemGate = make(chan struct{}, 1)

func acquireAccountGate() { accountRedeemGate <- struct{}{} }

func releaseAccountGate() {
	select {
	case <-accountRedeemGate:
	default:
	}
}

func init() { rand.Seed(time.Now().UnixNano()) }

// RedeemResult 兑换结果
type RedeemResult struct {
	Success           bool   `json:"success"`
	FID               string `json:"fid"`
	GiftCode          string `json:"gift_code"`
	CaptchaRecognized string `json:"captcha_recognized"`
	Message           string `json:"message"`
	Error             string `json:"error,omitempty"`
	ProcessingTime    int    `json:"processing_time"`
	Stage             string `json:"stage"`
	ErrCode           int    `json:"err_code,omitempty"`
	IsFatal           bool   `json:"is_fatal,omitempty"`
	Attempts          int    `json:"attempts"`
	Reward            string `json:"reward,omitempty"`
}

func NewAutomationService(gameClient *GameClient, ocrClient *OCRClient, logger *zap.Logger) *AutomationService {
	return &AutomationService{
		gameClient: gameClient,
		ocrClient:  ocrClient,
		logger:     logger,
	}
}

// VerifyAccount 验证账号有效性
func (s *AutomationService) VerifyAccount(fid string) (*RedeemResult, error) {
	result, err := s.gameClient.VerifyAccount(fid)
	if err != nil {
		return nil, err
	}

	return &RedeemResult{
		Success: result.Success,
		Error:   result.Error,
		ErrCode: result.ErrCode,
		FID:     fid,
	}, nil
}

// RedeemSingle 完整的单账号兑换流程（与Node版本对齐）
func (s *AutomationService) RedeemSingle(fid, giftCode string) (*RedeemResult, error) {
	// 降噪：流程级的开场使用调试级别
	s.logger.Debug("🚀 开始兑换流程",
		zap.String("fid", fid),
		zap.String("gift_code", giftCode))

	// 全局闸门：确保系统内任意时刻仅一个账号在兑换，避免验证码/频率被风控
	acquireAccountGate()
	defer releaseAccountGate()

	startTime := time.Now()

	// 1. 登录
	loginResult, err := s.gameClient.Login(fid)
	if err != nil {
		// 不向上抛出裸错误，转换为标准结果
		return &RedeemResult{
			Success:        false,
			FID:            fid,
			GiftCode:       giftCode,
			Error:          fmt.Sprintf("登录请求异常: %v", err),
			Stage:          "login_exception",
			ProcessingTime: int(time.Since(startTime).Seconds()),
		}, nil
	}

	if !loginResult.Success {
		return &RedeemResult{
			Success:        false,
			FID:            fid,
			GiftCode:       giftCode,
			Error:          fmt.Sprintf("登录失败: %s", loginResult.Error),
			Stage:          "login",
			ErrCode:        loginResult.ErrCode,
			ProcessingTime: int(time.Since(startTime).Seconds()),
		}, nil
	}

	// 2. 带重试的验证码识别和兑换过程
	maxRetries := 3
	var lastCaptchaValue string
	var lastError string

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// 降噪：重试轮次改为调试级别
		s.logger.Debug("📝 尝试验证码识别和兑换",
			zap.Int("attempt", attempt),
			zap.Int("max_retries", maxRetries))

		// 2.1 获取验证码（加小抖动以打散请求）
		time.Sleep(time.Duration(200+rand.Intn(600)) * time.Millisecond)
		captchaResult, err := s.gameClient.GetCaptcha()
		if err != nil {
			// 将异常视为服务器繁忙类问题，执行冷却+重登重试
			lastError = fmt.Sprintf("获取验证码异常: %v", err)
			if attempt == maxRetries {
				return &RedeemResult{
					Success:        false,
					FID:            fid,
					GiftCode:       giftCode,
					Error:          lastError,
					Stage:          "captcha_exception",
					ProcessingTime: int(time.Since(startTime).Seconds()),
					Attempts:       attempt,
				}, nil
			}

			s.logger.Warn("⏳ 获取验证码异常，可能服务器繁忙，冷却60秒后重试",
				zap.Int("attempt", attempt),
				zap.Int("max_retries", maxRetries),
				zap.Error(err))
			time.Sleep(60 * time.Second)

			// 冷却后重新登录
			reLoginResult, loginErr := s.gameClient.Login(fid)
			if loginErr != nil {
				return nil, loginErr
			}
			if !reLoginResult.Success {
				s.logger.Error("❌ 冷却后重新登录失败", zap.String("error", reLoginResult.Error))
				lastError = fmt.Sprintf("冷却后重新登录失败: %s", reLoginResult.Error)
				if attempt == maxRetries {
					return &RedeemResult{
						Success:        false,
						FID:            fid,
						GiftCode:       giftCode,
						Error:          lastError,
						Stage:          "relogin",
						ErrCode:        reLoginResult.ErrCode,
						ProcessingTime: int(time.Since(startTime).Seconds()),
						Attempts:       attempt,
					}, nil
				}
				s.logger.Debug("⚠️ 冷却后重新登录失败，继续重试...", zap.Int("attempt", attempt))
				continue
			}
			s.logger.Debug("✅ 冷却后重新登录成功，继续尝试获取验证码...")
			continue
		}

		if !captchaResult.Success {
			lastError = fmt.Sprintf("获取验证码失败: %s", captchaResult.Error)

			// 检查是否为验证码获取过多错误（40100）
			if captchaResult.ErrCode == 40100 {
				s.logger.Info("🔄 验证码获取过多，尝试重新登录", zap.String("fid", fid))

				// 重新登录
				reLoginResult, err := s.gameClient.Login(fid)
				if err != nil {
					return &RedeemResult{
						Success:        false,
						FID:            fid,
						GiftCode:       giftCode,
						Error:          fmt.Sprintf("重新登录请求异常: %v", err),
						Stage:          "relogin_exception",
						ProcessingTime: int(time.Since(startTime).Seconds()),
					}, nil
				}

				if !reLoginResult.Success {
					s.logger.Error("❌ 重新登录失败", zap.String("error", reLoginResult.Error))
					lastError = fmt.Sprintf("重新登录失败: %s", reLoginResult.Error)
					if attempt == maxRetries {
						// 达到本轮上限：进入一次“冷却60s+重登”的兜底流程
						s.logger.Warn("⏳ 重新登录仍失败，冷却60秒后再试一次...")
						time.Sleep(60 * time.Second)
						reLoginResult2, loginErr2 := s.gameClient.Login(fid)
						if loginErr2 != nil || !reLoginResult2.Success {
							return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: "重新登录失败(兜底)", Stage: "relogin", ProcessingTime: int(time.Since(startTime).Seconds())}, nil
						}
						continue
					}
					s.logger.Debug("⚠️ 重新登录失败，继续重试...", zap.Int("attempt", attempt))
					time.Sleep(3 * time.Second)
					continue
				}

				s.logger.Debug("✅ 重新登录成功，继续尝试获取验证码...")
				time.Sleep(3 * time.Second)
				continue
			}

			// 服务器繁忙（40101）：对账号冷却60s，重新登录后重试
			if captchaResult.ErrCode == 40101 {
				s.logger.Warn("⏳ 服务器繁忙，冷却60秒后重试获取验证码",
					zap.Int("attempt", attempt),
					zap.Int("max_retries", maxRetries))
				time.Sleep(60 * time.Second)

				// 冷却后重新登录
				reLoginResult, err := s.gameClient.Login(fid)
				if err != nil {
					return &RedeemResult{
						Success:        false,
						FID:            fid,
						GiftCode:       giftCode,
						Error:          fmt.Sprintf("冷却后重新登录请求异常: %v", err),
						Stage:          "relogin_exception",
						ProcessingTime: int(time.Since(startTime).Seconds()),
					}, nil
				}
				if !reLoginResult.Success {
					s.logger.Error("❌ 冷却后重新登录失败", zap.String("error", reLoginResult.Error))
					lastError = fmt.Sprintf("冷却后重新登录失败: %s", reLoginResult.Error)
					if attempt == maxRetries {
						return &RedeemResult{
							Success:        false,
							FID:            fid,
							GiftCode:       giftCode,
							Error:          lastError,
							Stage:          "relogin",
							ErrCode:        reLoginResult.ErrCode,
							ProcessingTime: int(time.Since(startTime).Seconds()),
							Attempts:       attempt,
						}, nil
					}
					// 进入下一轮尝试
					s.logger.Debug("⚠️ 冷却后重新登录失败，继续重试...", zap.Int("attempt", attempt))
					continue
				}
				// 重新登录成功，继续下一轮获取验证码
				s.logger.Debug("✅ 冷却后重新登录成功，继续尝试获取验证码...")
				continue
			}

			if attempt == maxRetries {
				return &RedeemResult{
					Success:        false,
					FID:            fid,
					GiftCode:       giftCode,
					Error:          lastError,
					Stage:          "captcha",
					ErrCode:        captchaResult.ErrCode,
					ProcessingTime: int(time.Since(startTime).Seconds()),
					Attempts:       attempt,
				}, nil
			}
			s.logger.Debug("⚠️ 获取验证码失败，继续重试...", zap.Int("attempt", attempt))
			time.Sleep(3 * time.Second)
			continue
		}

		// 2.2 OCR识别验证码（使用高精度版本）
		captchaData := captchaResult.Data.(map[string]interface{})
		captchaImg := captchaData["img"].(string)

		captchaValue, err := s.ocrClient.RecognizeCaptcha(captchaImg)
		if err != nil || captchaValue == "" {
			lastError = "验证码识别失败或长度异常"
			if attempt == maxRetries {
				// 达到本轮最大重试，进入“冷却+重登+再试”的流程一次
				s.logger.Warn("⏳ OCR 多次失败，冷却60秒并重新登录后再试一次...")
				time.Sleep(60 * time.Second)
				reLoginResult, loginErr := s.gameClient.Login(fid)
				if loginErr != nil {
					return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: fmt.Sprintf("冷却后重新登录请求异常: %v", loginErr), Stage: "relogin_exception", ProcessingTime: int(time.Since(startTime).Seconds())}, nil
				}
				if !reLoginResult.Success {
					return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: fmt.Sprintf("冷却后重新登录失败: %s", reLoginResult.Error), Stage: "relogin", ErrCode: reLoginResult.ErrCode, ProcessingTime: int(time.Since(startTime).Seconds())}, nil
				}
				// 冷却后再获取一次验证码，若仍失败将由下一轮逻辑兜底
				continue
			}
			// OCR 失败：冷却3秒后再获取新验证码，避免频率过快
			s.logger.Warn("⏳ 验证码识别失败，3秒后重试获取验证码...", zap.Int("attempt", attempt))
			time.Sleep(3 * time.Second)
			continue
		}

		// 规范化验证码：仅保留前4位字母数字并大写
		norm := make([]rune, 0, 4)
		for _, r := range captchaValue {
			if len(norm) >= 4 {
				break
			}
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				// 转大写
				if r >= 'a' && r <= 'z' {
					r = r - 'a' + 'A'
				}
				norm = append(norm, r)
			}
		}
		if len(norm) != 4 {
			lastError = "验证码识别失败或长度异常"
			if attempt == maxRetries {
				// 达到本轮最大重试，进入“冷却+重登+再试”的流程一次
				s.logger.Warn("⏳ 验证码长度异常多次，冷却60秒并重新登录后再试一次...")
				time.Sleep(60 * time.Second)
				reLoginResult, loginErr := s.gameClient.Login(fid)
				if loginErr != nil {
					return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: fmt.Sprintf("冷却后重新登录请求异常: %v", loginErr), Stage: "relogin_exception", ProcessingTime: int(time.Since(startTime).Seconds())}, nil
				}
				if !reLoginResult.Success {
					return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: fmt.Sprintf("冷却后重新登录失败: %s", reLoginResult.Error), Stage: "relogin", ErrCode: reLoginResult.ErrCode, ProcessingTime: int(time.Since(startTime).Seconds())}, nil
				}
				// 冷却后再获取一次验证码，若仍失败将由下一轮逻辑兜底
				continue
			}
			// 长度异常：同样冷却3秒再重试
			s.logger.Warn("⏳ 验证码长度异常，3秒后重试获取验证码...", zap.Int("attempt", attempt))
			time.Sleep(3 * time.Second)
			continue
		}
		captchaValue = string(norm)
		lastCaptchaValue = captchaValue

		// 2.3 执行兑换
		redeemResult, err := s.gameClient.RedeemCode(giftCode, captchaValue)
		if err != nil {
			// 视为服务器繁忙，走冷却+重登+重试
			lastError = fmt.Sprintf("兑换请求异常: %v", err)
			if attempt == maxRetries {
				return &RedeemResult{
					Success:           false,
					FID:               fid,
					GiftCode:          giftCode,
					CaptchaRecognized: captchaValue,
					Error:             lastError,
					ProcessingTime:    int(time.Since(startTime).Seconds()),
					Stage:             "redeem_exception",
					Attempts:          attempt,
				}, nil
			}

			s.logger.Warn("⏳ 兑换请求异常，可能服务器繁忙，冷却60秒后重试",
				zap.Int("attempt", attempt),
				zap.Int("max_retries", maxRetries),
				zap.Error(err))
			time.Sleep(60 * time.Second)

			// 冷却后重新登录
			reLoginResult, loginErr := s.gameClient.Login(fid)
			if loginErr != nil {
				return &RedeemResult{
					Success:           false,
					FID:               fid,
					GiftCode:          giftCode,
					CaptchaRecognized: captchaValue,
					Error:             fmt.Sprintf("冷却后重新登录请求异常: %v", loginErr),
					ProcessingTime:    int(time.Since(startTime).Seconds()),
					Stage:             "relogin_exception",
				}, nil
			}
			if !reLoginResult.Success {
				s.logger.Error("❌ 冷却后重新登录失败", zap.String("error", reLoginResult.Error))
				lastError = fmt.Sprintf("冷却后重新登录失败: %s", reLoginResult.Error)
				if attempt == maxRetries {
					return &RedeemResult{
						Success:           false,
						FID:               fid,
						GiftCode:          giftCode,
						CaptchaRecognized: captchaValue,
						Error:             lastError,
						ProcessingTime:    int(time.Since(startTime).Seconds()),
						Stage:             "relogin",
						ErrCode:           reLoginResult.ErrCode,
						Attempts:          attempt,
					}, nil
				}
				s.logger.Info("⚠️ 冷却后重新登录失败，继续重试...", zap.Int("attempt", attempt))
				continue
			}
			s.logger.Info("✅ 冷却后重新登录成功，继续重试兑换...")
			continue
		}

		if redeemResult.Success {
			// 兑换成功
			processingTime := int(time.Since(startTime).Seconds())
			s.logger.Debug("✅ 兑换成功！", zap.Int("attempt", attempt))

			redeemData := redeemResult.Data.(map[string]interface{})
			reward := ""
			if r, ok := redeemData["reward"]; ok {
				reward = r.(string)
			}

			return &RedeemResult{
				Success:           true,
				FID:               fid,
				GiftCode:          giftCode,
				CaptchaRecognized: captchaValue,
				Message:           "兑换成功",
				Reward:            reward,
				ProcessingTime:    processingTime,
				Stage:             "completed",
				ErrCode:           redeemResult.ErrCode,
				Attempts:          attempt,
			}, nil
		} else {
			lastError = redeemResult.Error

			// 检查是否为致命错误（不需要重试）
			if redeemResult.IsFatal {
				s.logger.Info("💀 遇到致命错误，停止重试", zap.String("error", redeemResult.Error))
				processingTime := int(time.Since(startTime).Seconds())
				return &RedeemResult{
					Success:           false,
					FID:               fid,
					GiftCode:          giftCode,
					CaptchaRecognized: captchaValue,
					Error:             redeemResult.Error,
					ProcessingTime:    processingTime,
					Stage:             "redeem",
					ErrCode:           redeemResult.ErrCode,
					IsFatal:           true,
					Attempts:          attempt,
				}, nil
			}

			// 检查是否为验证码错误或验证码过期（40103/40102），均视为需要重新获取验证码
			if redeemResult.ErrCode == 40103 || redeemResult.ErrCode == 40102 {
				if attempt == maxRetries {
					// 达到本轮上限，再进行一次“冷却60s+重新登录”的兜底后再试一次
					s.logger.Warn("❌ 验证码类错误达到最大重试次数，将冷却60秒并重新登录后再试一次")
					time.Sleep(60 * time.Second)
					reLoginResult, loginErr := s.gameClient.Login(fid)
					if loginErr != nil || !reLoginResult.Success {
						s.logger.Error("❌ 冷却后重新登录失败(验证码类兜底)")
						// 兜底也失败，返回
						break
					}
					// 兜底成功，继续下一轮（外层 for 会迭代）
				} else {
					s.logger.Debug("🔄 验证码错误/过期，3秒后重新获取验证码...", zap.Int("attempt", attempt))
					time.Sleep(3 * time.Second)
					continue
				}
			} else if redeemResult.ErrCode == 40101 { // 服务器繁忙
				if attempt == maxRetries {
					s.logger.Warn("❌ 服务器繁忙，已达到最大重试次数",
						zap.Int("attempt", attempt),
						zap.Int("max_retries", maxRetries))
				} else {
					s.logger.Warn("⏳ 服务器繁忙，冷却60秒后重试兑换",
						zap.Int("attempt", attempt),
						zap.Int("max_retries", maxRetries))
					time.Sleep(60 * time.Second)

					// 冷却后重新登录
					reLoginResult, err := s.gameClient.Login(fid)
					if err != nil {
						return &RedeemResult{
							Success:           false,
							FID:               fid,
							GiftCode:          giftCode,
							CaptchaRecognized: captchaValue,
							Error:             fmt.Sprintf("冷却后重新登录请求异常: %v", err),
							ProcessingTime:    int(time.Since(startTime).Seconds()),
							Stage:             "relogin_exception",
						}, nil
					}
					if !reLoginResult.Success {
						s.logger.Error("❌ 冷却后重新登录失败", zap.String("error", reLoginResult.Error))
						lastError = fmt.Sprintf("冷却后重新登录失败: %s", reLoginResult.Error)
						if attempt == maxRetries {
							processingTime := int(time.Since(startTime).Seconds())
							return &RedeemResult{
								Success:           false,
								FID:               fid,
								GiftCode:          giftCode,
								CaptchaRecognized: captchaValue,
								Error:             lastError,
								ProcessingTime:    processingTime,
								Stage:             "relogin",
								ErrCode:           reLoginResult.ErrCode,
								Attempts:          attempt,
							}, nil
						}
						// 进入下一轮尝试
						s.logger.Debug("⚠️ 冷却后重新登录失败，继续重试...", zap.Int("attempt", attempt))
						continue
					}
					// 重新登录成功，进入下一轮重试（会重新获取验证码并兑换）
					s.logger.Debug("✅ 冷却后重新登录成功，继续重试兑换...")
					continue
				}
			} else {
				// 其他错误，直接返回
				s.logger.Info("❌ 兑换失败 (非验证码问题)", zap.String("error", redeemResult.Error))
				processingTime := int(time.Since(startTime).Seconds())
				return &RedeemResult{
					Success:           false,
					FID:               fid,
					GiftCode:          giftCode,
					CaptchaRecognized: captchaValue,
					Error:             redeemResult.Error,
					ProcessingTime:    processingTime,
					Stage:             "redeem",
					ErrCode:           redeemResult.ErrCode,
					IsFatal:           redeemResult.IsFatal,
					Attempts:          attempt,
				}, nil
			}
		}
	}

	// 所有重试都失败了
	s.logger.Info("❌ 所有重试都失败了", zap.Int("max_retries", maxRetries))
	processingTime := int(time.Since(startTime).Seconds())
	return &RedeemResult{
		Success:           false,
		FID:               fid,
		GiftCode:          giftCode,
		CaptchaRecognized: lastCaptchaValue,
		Error:             lastError,
		ProcessingTime:    processingTime,
		Stage:             "retry_exhausted",
		Attempts:          maxRetries,
	}, nil
}

// RedeemBatch 批量兑换（复刻Node版本逻辑）
func (s *AutomationService) RedeemBatch(accounts []Account, giftCode string) ([]BatchRedeemResult, error) {
	// 新的调度器：避免在单账号内阻塞60秒冷却；将需要冷却的账号延后至队列末尾，并在所有可处理账号完成后再回头处理
	type accountState struct {
		acc             Account
		cooldowns       int // 已发生的60s冷却次数
		attemptsInCycle int // 自上次冷却以来的非冷却尝试次数（用于3次后触发一次冷却）
		nextReadyAt     time.Time
		finalized       bool
	}

	s.logger.Info("📦 开始批量兑换(调度)",
		zap.Int("accounts_count", len(accounts)),
		zap.String("gift_code", giftCode))

	states := make([]*accountState, 0, len(accounts))
	for _, a := range accounts {
		states = append(states, &accountState{acc: a, nextReadyAt: time.Now()})
	}

	results := make([]BatchRedeemResult, 0, len(accounts))
	pending := len(states)
	// 账号切换的最小间隔，避免切换过快触发风控
	minSwitchDelay := 3 * time.Second
	lastSwitchAt := time.Time{}

	// 选择下一个可执行的账号索引；若都在冷却，返回最早可执行的索引与需等待时长
	pickNext := func(now time.Time) (idx int, wait time.Duration, found bool) {
		earliestIdx := -1
		earliestTime := time.Time{}
		for i, st := range states {
			if st.finalized {
				continue
			}
			if !st.nextReadyAt.After(now) {
				return i, 0, true
			}
			if earliestIdx == -1 || st.nextReadyAt.Before(earliestTime) {
				earliestIdx = i
				earliestTime = st.nextReadyAt
			}
		}
		if earliestIdx == -1 {
			return -1, 0, false
		}
		return earliestIdx, time.Until(earliestTime), true
	}

	for pending > 0 {
		now := time.Now()
		idx, wait, ok := pickNext(now)
		if !ok {
			break // 理论上不会发生
		}
		if wait > 0 {
			// 所有账号均在冷却：仅等待到最早可执行时间，避免空转
			if wait > 0 {
				s.logger.Debug("⏳ 所有账号冷却中，等待下一可执行窗口", zap.Duration("wait", wait))
				time.Sleep(wait)
			}
			continue
		}

		// 账号切换最小节流：与上次尝试间隔不足3秒，则补足
		if !lastSwitchAt.IsZero() {
			since := time.Since(lastSwitchAt)
			if since < minSwitchDelay {
				sleep := minSwitchDelay - since
				s.logger.Debug("⏳ 账号切换节流等待", zap.Duration("wait", sleep))
				time.Sleep(sleep)
			}
		}

		st := states[idx]

		// 单次尝试（不在内部执行60s睡眠）
		stepStart := time.Now()
		stepRes := s.tryOnceNoCooldown(st.acc.FID, giftCode)
		procSec := int(time.Since(stepStart).Seconds())
		lastSwitchAt = time.Now()

		// 构造临时结果（仅在最终确定时append）
		tmp := BatchRedeemResult{
			AccountID:         st.acc.ID,
			FID:               st.acc.FID,
			Result:            "failed",
			Error:             stepRes.Error,
			CaptchaRecognized: stepRes.CaptchaRecognized,
			ProcessingTime:    procSec,
			ErrCode:           stepRes.ErrCode,
			Success:           stepRes.Success,
		}

		if stepRes.Success {
			s.logger.Info("✅ 账号兑换成功",
				zap.String("fid", st.acc.FID),
				zap.String("code", giftCode))
			tmp.Result = "success"
			tmp.Error = ""
			results = append(results, tmp)
			st.finalized = true
			pending--
			continue
		}

		// 致命错误：直接终止该账号
		if stepRes.IsFatal {
			results = append(results, tmp)
			st.finalized = true
			pending--
			continue
		}

		// 分类处理：根据错误码进行调度（不在这里睡60s）
		switch stepRes.ErrCode {
		case 40101: // 服务器繁忙 → 冷却60s并重置本轮计数
			st.cooldowns++
			st.attemptsInCycle = 0
			if st.cooldowns >= 3 {
				// 超过3次冷却依然失败
				s.logger.Warn("❌ 账号多次冷却仍失败",
					zap.String("fid", st.acc.FID), zap.Int("cooldowns", st.cooldowns))
				results = append(results, tmp)
				st.finalized = true
				pending--
			} else {
				st.nextReadyAt = time.Now().Add(60 * time.Second)
				s.logger.Warn("⏳ 服务器繁忙，账号进入冷却队列", zap.String("fid", st.acc.FID), zap.Int("cooldowns", st.cooldowns))
			}
		case 40102, 40103: // 验证码过期/错误 → 3次内快速重试；超过3次触发一次60s冷却
			st.attemptsInCycle++
			if st.attemptsInCycle >= 3 {
				st.cooldowns++
				st.attemptsInCycle = 0
				if st.cooldowns >= 3 {
					s.logger.Warn("❌ 账号验证码问题多次冷却仍失败",
						zap.String("fid", st.acc.FID), zap.Int("cooldowns", st.cooldowns))
					results = append(results, tmp)
					st.finalized = true
					pending--
				} else {
					st.nextReadyAt = time.Now().Add(60 * time.Second)
					s.logger.Warn("⏳ 验证码错误多次，账号进入冷却队列", zap.String("fid", st.acc.FID), zap.Int("cooldowns", st.cooldowns))
				}
			} else {
				st.nextReadyAt = time.Now().Add(3 * time.Second)
				s.logger.Debug("🔄 验证码问题，短暂冷却后重试", zap.String("fid", st.acc.FID), zap.Int("attempt_in_cycle", st.attemptsInCycle))
			}
		case 40100: // 验证码获取过多 → 视为短暂退避
			st.attemptsInCycle++
			st.nextReadyAt = time.Now().Add(3 * time.Second)
			s.logger.Debug("🔁 验证码获取过多，短暂退避", zap.String("fid", st.acc.FID))
		default:
			// 其他错误：视为终止（避免无休止重试），直接记失败
			s.logger.Error("❌ 账号兑换失败(非致命)",
				zap.String("fid", st.acc.FID),
				zap.String("code", giftCode),
				zap.String("error", stepRes.Error),
				zap.Int("err_code", stepRes.ErrCode))
			results = append(results, tmp)
			st.finalized = true
			pending--
		}
	}

	// 统计
	successCount := 0
	for _, r := range results {
		if r.Result == "success" {
			successCount++
		}
	}
	s.logger.Info("📊 批量兑换完成(调度)",
		zap.Int("success", successCount),
		zap.Int("total", len(results)))

	return results, nil
}

// tryOnceNoCooldown 单次尝试，不在内部执行60s冷却等待；需要外层调度器根据返回的错误码进行队列冷却
func (s *AutomationService) tryOnceNoCooldown(fid, giftCode string) *RedeemResult {
	startTime := time.Now()

	// 1. 登录（失败直接分类返回）
	loginResult, err := s.gameClient.Login(fid)
	if err != nil {
		return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: "登录请求异常", Stage: "login_exception", Attempts: 1}
	}
	if !loginResult.Success {
		// 服务器繁忙→交由外层冷却；致命→直接失败
		if loginResult.ErrCode == 40101 {
			return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: loginResult.Error, Stage: "login", ErrCode: 40101}
		}
		return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: loginResult.Error, Stage: "login", ErrCode: loginResult.ErrCode, IsFatal: s.gameClient.isFatalError(loginResult.ErrCode)}
	}

	// 2. 获取验证码
	time.Sleep(time.Duration(200+rand.Intn(600)) * time.Millisecond)
	captchaResult, err := s.gameClient.GetCaptcha()
	if err != nil {
		// 视为服务器繁忙类问题
		return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: "获取验证码异常", Stage: "captcha_exception", ErrCode: 40101}
	}
	if !captchaResult.Success {
		if captchaResult.ErrCode == 40101 {
			return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: captchaResult.Error, Stage: "captcha", ErrCode: 40101}
		}
		// 40100 过多，作为短暂退避
		if captchaResult.ErrCode == 40100 {
			return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: captchaResult.Error, Stage: "captcha", ErrCode: 40100}
		}
		return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: captchaResult.Error, Stage: "captcha", ErrCode: captchaResult.ErrCode}
	}

	// 3. OCR识别
	captchaData := captchaResult.Data.(map[string]interface{})
	captchaImg := captchaData["img"].(string)
	captchaValue, err := s.ocrClient.RecognizeCaptcha(captchaImg)
	if err != nil || captchaValue == "" {
		return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: "验证码识别失败", Stage: "ocr", ErrCode: 40103}
	}
	// 规范化为4位
	norm := make([]rune, 0, 4)
	for _, r := range captchaValue {
		if len(norm) >= 4 {
			break
		}
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			if r >= 'a' && r <= 'z' {
				r = r - 'a' + 'A'
			}
			norm = append(norm, r)
		}
	}
	if len(norm) != 4 {
		return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: "验证码长度异常", Stage: "ocr", ErrCode: 40103}
	}
	captchaValue = string(norm)

	// 4. 兑换
	redeemResult, err := s.gameClient.RedeemCode(giftCode, captchaValue)
	if err != nil {
		// 视为服务器繁忙类问题
		return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, Error: "兑换请求异常", Stage: "redeem_exception", ErrCode: 40101}
	}
	if redeemResult.Success {
		processingTime := int(time.Since(startTime).Seconds())
		return &RedeemResult{Success: true, FID: fid, GiftCode: giftCode, CaptchaRecognized: captchaValue, Message: "兑换成功", ProcessingTime: processingTime, Stage: "completed", ErrCode: redeemResult.ErrCode, Attempts: 1}
	}

	// 分类错误
	if redeemResult.ErrCode == 40101 { // 服务器繁忙
		return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, CaptchaRecognized: captchaValue, Error: redeemResult.Error, Stage: "redeem", ErrCode: 40101}
	}
	if redeemResult.ErrCode == 40102 || redeemResult.ErrCode == 40103 { // 验证码问题
		return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, CaptchaRecognized: captchaValue, Error: redeemResult.Error, Stage: "redeem", ErrCode: redeemResult.ErrCode}
	}
	if redeemResult.IsFatal {
		return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, CaptchaRecognized: captchaValue, Error: redeemResult.Error, Stage: "redeem", ErrCode: redeemResult.ErrCode, IsFatal: true}
	}
	// 其他错误直接返回失败，交由上层记录日志
	return &RedeemResult{Success: false, FID: fid, GiftCode: giftCode, CaptchaRecognized: captchaValue, Error: redeemResult.Error, Stage: "redeem", ErrCode: redeemResult.ErrCode}
}

// BatchRedeemResult 批量兑换结果
type BatchRedeemResult struct {
	AccountID         int    `json:"accountId"`
	FID               string `json:"fid"`
	Result            string `json:"result"`
	Error             string `json:"error"`
	CaptchaRecognized string `json:"captchaRecognized"`
	ProcessingTime    int    `json:"processingTime"`
	ErrCode           int    `json:"errCode"`
	Success           bool   `json:"success"`
	Skipped           bool   `json:"skipped,omitempty"`
}

// Account 简化的账号模型用于批量兑换
type Account struct {
	ID  int    `json:"id"`
	FID string `json:"fid"`
}

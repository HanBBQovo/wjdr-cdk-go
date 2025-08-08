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
	results := make([]BatchRedeemResult, 0, len(accounts))
	fatalError := false

	s.logger.Info("📦 开始批量兑换",
		zap.Int("accounts_count", len(accounts)),
		zap.String("gift_code", giftCode))

	for _, account := range accounts {
		startTime := time.Now()
		result, err := s.RedeemSingle(account.FID, giftCode)
		if err != nil {
			s.logger.Error("❌ 账号兑换异常",
				zap.String("fid", account.FID),
				zap.Error(err))

			results = append(results, BatchRedeemResult{
				AccountID:      account.ID,
				FID:            account.FID,
				Result:         "failed",
				Error:          err.Error(),
				ProcessingTime: int(time.Since(startTime).Seconds()),
			})
			continue
		}

		processingTime := int(time.Since(startTime).Seconds())
		batchResult := BatchRedeemResult{
			AccountID:         account.ID,
			FID:               account.FID,
			Result:            "failed",
			Error:             result.Error,
			CaptchaRecognized: result.CaptchaRecognized,
			ProcessingTime:    processingTime,
			ErrCode:           result.ErrCode,
			Success:           result.Success,
		}

		if result.Success {
			batchResult.Result = "success"
			batchResult.Error = ""
		}

		results = append(results, batchResult)

		// 检查是否为致命错误
		if result.IsFatal {
			fatalError = true
			s.logger.Warn("⚠️ 检测到致命错误，停止处理剩余账号",
				zap.Int("err_code", result.ErrCode),
				zap.String("error", result.Error))

			// 为剩余账号填充相同错误结果
			for i := len(results); i < len(accounts); i++ {
				results = append(results, BatchRedeemResult{
					AccountID:         accounts[i].ID,
					FID:               accounts[i].FID,
					Result:            "failed",
					Error:             result.Error,
					CaptchaRecognized: "",
					ProcessingTime:    0,
					ErrCode:           result.ErrCode,
					Success:           false,
					Skipped:           true,
				})
			}
			// 已构造所有剩余账号的结果
			break
		}

		s.logger.Debug("✅ 账号处理完成",
			zap.Int("completed", len(results)),
			zap.Int("total", len(accounts)),
			zap.String("fid", account.FID),
			zap.String("result", batchResult.Result))

		// 如果不是致命错误：账号之间动态延时
		// 基础 3s + 抖动(250-750ms)。若上一结果为 40100/40101/40102/40103，指数退避：额外等待 (2^retries)s，retries 由 Attempts 推算。
		if len(results) < len(accounts) {
			base := 3*time.Second + time.Duration(250+rand.Intn(500))*time.Millisecond
			extra := time.Duration(0)
			if result != nil && !result.Success {
				switch result.ErrCode {
				case 40100, 40101, 40102, 40103:
					// Attempts 至少为1
					retries := result.Attempts
					if retries < 1 {
						retries = 1
					}
					// 2^(retries-1) 秒，最大 30s
					pow := 1 << (retries - 1)
					if pow > 30 {
						pow = 30
					}
					extra = time.Duration(pow) * time.Second
				}
			}
			delay := base + extra
			s.logger.Debug("⏳ 账号切换延时", zap.Duration("delay", delay), zap.Int("attempts", result.Attempts), zap.Int("err_code", result.ErrCode))
			time.Sleep(delay)
		}
	}

	successCount := 0
	skippedCount := 0
	for _, r := range results {
		if r.Result == "success" {
			successCount++
		}
		if r.Skipped {
			skippedCount++
		}
	}

	if fatalError {
		s.logger.Info("📊 批量兑换完成（致命错误）",
			zap.Int("success", successCount),
			zap.Int("total", len(results)),
			zap.Int("skipped", skippedCount))
	} else {
		s.logger.Info("📊 批量兑换完成",
			zap.Int("success", successCount),
			zap.Int("total", len(results)))
	}

	return results, nil
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

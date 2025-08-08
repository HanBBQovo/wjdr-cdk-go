package client

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// GameClient 游戏API客户端（完全复刻Node版本逻辑）
type GameClient struct {
	salt     string
	baseURL  string
	fid      string
	nickname string
	client   *http.Client
	logger   *zap.Logger
}

// GameResponse 游戏API通用响应
type GameResponse struct {
	Code    int         `json:"code"`
	ErrCode interface{} `json:"err_code"` // 游戏API可能返回字符串或数字
	Msg     interface{} `json:"msg"`      // 可能为字符串或数字
	Data    interface{} `json:"data"`
}

// LoginData 登录响应数据
type LoginData struct {
	FID                 string `json:"fid"`
	Nickname            string `json:"nickname"`
	AvatarImage         string `json:"avatar_image"`
	StoveLv             int    `json:"stove_lv"`
	StoveLvContent      string `json:"stove_lv_content"`
	Kid                 string `json:"kid"`
	TotalRechargeAmount int    `json:"total_recharge_amount"`
}

// CaptchaData 验证码响应数据
type CaptchaData struct {
	Img string `json:"img"`
}

// RedeemData 兑换响应数据
type RedeemData struct {
	Reward  string `json:"reward"`
	Message string `json:"message"`
}

// GameResult 游戏操作结果
type GameResult struct {
	Success           bool        `json:"success"`
	Data              interface{} `json:"data,omitempty"`
	Error             string      `json:"error,omitempty"`
	ErrCode           int         `json:"errCode,omitempty"`
	Stage             string      `json:"stage,omitempty"`
	CaptchaRecognized string      `json:"captcha_recognized,omitempty"`
	ProcessingTime    int         `json:"processing_time,omitempty"`
	IsFatal           bool        `json:"is_fatal,omitempty"`
	Attempts          int         `json:"attempts,omitempty"`
}

func NewGameClient(logger *zap.Logger) *GameClient {
	return &GameClient{
		salt:    "Uiv#87#SPan.ECsp", // 与Node版本一致
		baseURL: "https://wjdr-giftcode-api.campfiregames.cn/api",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// parseErrCode 将游戏API的 err_code（可能为string或number）转换为int
func (c *GameClient) parseErrCode(errCodeAny interface{}) int {
	switch v := errCodeAny.(type) {
	case nil:
		return 0
	case float64:
		return int(v)
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case string:
		if v == "" {
			return 0
		}
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		c.logger.Warn("无法解析错误码", zap.String("err_code_str", v))
		return 0
	default:
		// 尝试格式化为字符串再解析
		s := fmt.Sprintf("%v", v)
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		c.logger.Warn("未知err_code类型", zap.String("type", fmt.Sprintf("%T", v)))
		return 0
	}
}

// getErrorMessage 错误码映射（与Node版本对齐）
func (c *GameClient) getErrorMessage(errCode int) string {
	errorMap := map[int]string{
		20000: "兑换成功",
		40005: "超出领取次数",
		40006: "不满足活动领取条件",
		40007: "兑换码已过期",
		40008: "已经兑换过此礼品码",
		40011: "已兑换过同类型兑换码",
		40009: "登录状态失效",
		40014: "兑换码不存在",
		40102: "验证码过期",
		40100: "验证码获取过多",
		40101: "服务器繁忙",
		40103: "验证码错误",
		40001: "参数错误",
		40002: "签名错误",
	}

	if msg, exists := errorMap[errCode]; exists {
		return msg
	}
	return fmt.Sprintf("未知错误 (%d)", errCode)
}

// messageToString 将可能为数字/字符串/空值的 msg 转为字符串
func (c *GameClient) messageToString(msg interface{}) string {
	switch v := msg.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// isFatalError 检查是否为致命错误（与Node版本对齐）
func (c *GameClient) isFatalError(errCode int) bool {
	return errCode == 40007 || errCode == 40014 // 兑换码过期或不存在
}

// isSuccess 检查是否为成功（与Node版本对齐）
func (c *GameClient) isSuccess(errCode int) bool {
	return errCode == 20000 // 兑换成功
}

// generateSign 生成签名（与Node版本完全对齐）
func (c *GameClient) generateSign(fid, timeMs string, init, cdk, captchaCode *string) string {
	params := map[string]string{
		"fid":  fid,
		"time": timeMs,
	}

	if init != nil {
		params["init"] = *init
	}
	if cdk != nil {
		params["cdk"] = *cdk
	}
	if captchaCode != nil {
		params["captcha_code"] = *captchaCode
	}

	// 对参数键进行字母顺序排序（与Node逻辑一致）
	var sortedKeys []string
	for key := range params {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	// 构建参数字符串
	var sortedParams []string
	for _, key := range sortedKeys {
		sortedParams = append(sortedParams, fmt.Sprintf("%s=%s", key, params[key]))
	}

	signString := strings.Join(sortedParams, "&") + c.salt

	// 生成MD5哈希
	hash := md5.Sum([]byte(signString))
	return fmt.Sprintf("%x", hash)
}

// Login 登录验证（与Node版本对齐）
func (c *GameClient) Login(fid string) (*GameResult, error) {
	currentTime := strconv.FormatInt(time.Now().UnixMilli(), 10)
	init := "1"
	sign := c.generateSign(fid, currentTime, &init, nil, nil)

	data := url.Values{}
	data.Set("fid", fid)
	data.Set("time", currentTime)
	data.Set("init", "1")
	data.Set("sign", sign)

	// 降噪：登录开始改为调试级别
	c.logger.Debug("🔐 登录验证", zap.String("fid", fid))

	req, err := http.NewRequest("POST", c.baseURL+"/player", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Error("❌ 登录请求异常", zap.Error(err))
		return &GameResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Error("❌ 登录响应读取失败", zap.Error(err))
		return &GameResult{Success: false, Error: "服务器繁忙", ErrCode: 40101}, nil
	}
	if resp.StatusCode != http.StatusOK {
		c.logger.Error("❌ 登录HTTP状态异常",
			zap.Int("status", resp.StatusCode))
		return &GameResult{Success: false, Error: "服务器繁忙", ErrCode: 40101}, nil
	}

	var gameResp GameResponse
	if err := json.Unmarshal(body, &gameResp); err != nil {
		c.logger.Error("❌ 登录响应解析失败",
			zap.Error(err))
		return &GameResult{Success: false, Error: "服务器繁忙", ErrCode: 40101}, nil
	}

	if gameResp.Code == 0 {
		c.fid = fid

		// 解析用户数据
		dataBytes, _ := json.Marshal(gameResp.Data)
		var userData LoginData
		json.Unmarshal(dataBytes, &userData)
		c.nickname = userData.Nickname

		// 降噪：登录成功改为调试级别
		c.logger.Debug("✅ 登录成功！",
			zap.String("user", userData.Nickname),
			zap.String("fid", fid),
			zap.Int("level", userData.StoveLv))

		return &GameResult{
			Success: true,
			Data: map[string]interface{}{
				"fid":                   userData.FID,
				"nickname":              userData.Nickname,
				"avatar_image":          userData.AvatarImage,
				"stove_lv":              userData.StoveLv,
				"stove_lv_content":      userData.StoveLvContent,
				"kid":                   userData.Kid,
				"total_recharge_amount": userData.TotalRechargeAmount,
				"timestamp":             currentTime,
			},
		}, nil
	} else {
		errCodeInt := c.parseErrCode(gameResp.ErrCode)
		errorText := c.getErrorMessage(errCodeInt)
		if errorText == "" {
			errorText = c.messageToString(gameResp.Msg)
		}
		if errorText == "" {
			errorText = "登录失败"
		}

		c.logger.Error("❌ 登录失败",
			zap.String("error", errorText),
			zap.Int("err_code", errCodeInt))

		return &GameResult{
			Success: false,
			Error:   errorText,
			ErrCode: errCodeInt,
		}, nil
	}
}

// GetCaptcha 获取验证码（与Node版本对齐）
func (c *GameClient) GetCaptcha() (*GameResult, error) {
	currentTime := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := c.generateSign(c.fid, currentTime, nil, nil, nil)

	data := url.Values{}
	data.Set("fid", c.fid)
	data.Set("time", currentTime)
	data.Set("sign", sign)

	// 降噪：获取验证码改为调试级别
	c.logger.Debug("🔍 获取验证码...",
		zap.String("fid", c.fid),
		zap.String("user", c.nickname))

	req, err := http.NewRequest("POST", c.baseURL+"/captcha", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Error("❌ 获取验证码异常", zap.Error(err))
		return &GameResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Error("❌ 验证码响应读取失败", zap.Error(err))
		return &GameResult{Success: false, Error: "服务器繁忙", ErrCode: 40101}, nil
	}
	if resp.StatusCode != http.StatusOK {
		c.logger.Error("❌ 获取验证码HTTP状态异常",
			zap.Int("status", resp.StatusCode))
		return &GameResult{Success: false, Error: "服务器繁忙", ErrCode: 40101}, nil
	}

	var gameResp GameResponse
	if err := json.Unmarshal(body, &gameResp); err != nil {
		c.logger.Error("❌ 获取验证码响应解析失败",
			zap.Error(err))
		return &GameResult{Success: false, Error: "服务器繁忙", ErrCode: 40101}, nil
	}

	if gameResp.Code == 0 {
		// 降噪：验证码获取成功改为调试级别
		c.logger.Debug("✅ 验证码获取成功")

		// 解析验证码数据
		dataBytes, _ := json.Marshal(gameResp.Data)
		var captchaData CaptchaData
		json.Unmarshal(dataBytes, &captchaData)

		return &GameResult{
			Success: true,
			Data: map[string]interface{}{
				"img": captchaData.Img,
			},
		}, nil
	} else {
		errCodeInt := c.parseErrCode(gameResp.ErrCode)
		errorText := c.getErrorMessage(errCodeInt)
		if errorText == "" {
			errorText = c.messageToString(gameResp.Msg)
		}
		if errorText == "" {
			errorText = "获取验证码失败"
		}

		if errCodeInt == 40100 {
			c.logger.Warn("⚠️ 验证码获取过多，需要重新登录",
				zap.String("error", errorText),
				zap.Int("err_code", errCodeInt),
				zap.String("fid", c.fid),
				zap.String("user", c.nickname))
		} else {
			c.logger.Error("❌ 获取验证码失败",
				zap.String("error", errorText),
				zap.Int("err_code", errCodeInt),
				zap.String("fid", c.fid),
				zap.String("user", c.nickname))
		}

		return &GameResult{
			Success: false,
			Error:   errorText,
			ErrCode: errCodeInt,
		}, nil
	}
}

// RedeemCode 兑换礼品码（与Node版本对齐）
func (c *GameClient) RedeemCode(giftCode, captchaValue string) (*GameResult, error) {
	currentTime := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := c.generateSign(c.fid, currentTime, nil, &giftCode, &captchaValue)

	data := url.Values{}
	data.Set("fid", c.fid)
	data.Set("time", currentTime)
	data.Set("cdk", giftCode)
	data.Set("captcha_code", captchaValue)
	data.Set("sign", sign)

	// 降噪：兑换动作改为调试级别
	c.logger.Debug("🎁 兑换礼品码",
		zap.String("code", giftCode),
		zap.String("captcha", captchaValue),
		zap.String("fid", c.fid),
		zap.String("user", c.nickname))

	req, err := http.NewRequest("POST", c.baseURL+"/gift_code", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Error("❌ 兑换请求异常", zap.Error(err))
		return &GameResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Error("❌ 兑换响应读取失败", zap.Error(err))
		return &GameResult{Success: false, Error: "服务器繁忙", ErrCode: 40101}, nil
	}
	if resp.StatusCode != http.StatusOK {
		c.logger.Error("❌ 兑换HTTP状态异常",
			zap.Int("status", resp.StatusCode))
		return &GameResult{Success: false, Error: "服务器繁忙", ErrCode: 40101}, nil
	}

	var gameResp GameResponse
	if err := json.Unmarshal(body, &gameResp); err != nil {
		c.logger.Error("❌ 兑换响应解析失败",
			zap.Error(err))
		return &GameResult{Success: false, Error: "服务器繁忙", ErrCode: 40101}, nil
	}

	errCodeInt := c.parseErrCode(gameResp.ErrCode)
	isSuccess := c.isSuccess(errCodeInt)

	if isSuccess {
		// 解析兑换奖励数据
		dataBytes, _ := json.Marshal(gameResp.Data)
		var redeemData RedeemData
		json.Unmarshal(dataBytes, &redeemData)

		// 兑换成功改为 info 并带上用户标识
		c.logger.Info("✅ 兑换成功",
			zap.String("reward", redeemData.Reward),
			zap.String("fid", c.fid),
			zap.String("user", c.nickname),
			zap.String("code", giftCode))

		return &GameResult{
			Success: true,
			Data: map[string]interface{}{
				"reward":  redeemData.Reward,
				"message": redeemData.Message,
			},
			ErrCode: errCodeInt,
		}, nil
	} else {
		errorText := c.getErrorMessage(errCodeInt)

		msgStr := c.messageToString(gameResp.Msg)
		if strings.HasPrefix(errorText, "未知错误") && msgStr != "" {
			errorText = fmt.Sprintf("%s | msg: %s", errorText, msgStr)
		}
		if errorText == "" {
			errorText = msgStr
		}
		if errorText == "" {
			errorText = "兑换失败"
		}

		// 根据错误码提供详细信息（与Node逻辑一致）
		switch errCodeInt {
		case 40005:
			c.logger.Info("🚫 账号超出领取次数", zap.String("code", giftCode), zap.String("fid", c.fid), zap.String("user", c.nickname))
		case 40006:
			c.logger.Info("🎯 不满足活动领取条件", zap.String("code", giftCode), zap.String("fid", c.fid), zap.String("user", c.nickname))
		case 40008:
			c.logger.Info("💫 账号已兑换过", zap.String("code", giftCode), zap.String("fid", c.fid), zap.String("user", c.nickname))
		case 40011:
			c.logger.Info("🔄 账号已兑换过同类型兑换码", zap.String("code", giftCode), zap.String("fid", c.fid), zap.String("user", c.nickname))
		case 40103:
			c.logger.Error("🤖 验证码识别错误",
				zap.String("captcha", captchaValue),
				zap.String("error", errorText),
				zap.String("fid", c.fid),
				zap.String("user", c.nickname))
		case 40009:
			c.logger.Error("🔐 登录状态失效", zap.String("error", errorText), zap.String("fid", c.fid), zap.String("user", c.nickname))
		case 40101:
			c.logger.Error("🔄 服务器繁忙", zap.String("error", errorText), zap.String("fid", c.fid), zap.String("user", c.nickname))
		case 40007:
			c.logger.Error("⏰ 兑换码已过期", zap.String("code", giftCode), zap.String("fid", c.fid), zap.String("user", c.nickname))
		case 40014:
			c.logger.Error("❓ 兑换码不存在", zap.String("code", giftCode), zap.String("fid", c.fid), zap.String("user", c.nickname))
		default:
			c.logger.Error("❌ 兑换失败",
				zap.String("error", errorText),
				zap.Int("err_code", errCodeInt),
				zap.String("fid", c.fid),
				zap.String("user", c.nickname))
		}

		return &GameResult{
			Success: false,
			Error:   errorText,
			ErrCode: errCodeInt,
			IsFatal: c.isFatalError(errCodeInt),
		}, nil
	}
}

// VerifyAccount 验证账号有效性（与Node版本对齐）
func (c *GameClient) VerifyAccount(fid string) (*GameResult, error) {
	return c.Login(fid)
}

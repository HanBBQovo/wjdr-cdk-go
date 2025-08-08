package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// OCRClient 百度OCR客户端（完全复刻Node版本逻辑）
type OCRClient struct {
	apiKey         string
	secretKey      string
	accessToken    string
	tokenExpiresAt int64
	client         *http.Client
	logger         *zap.Logger
	mutex          sync.RWMutex
}

// OCRTokenResponse 百度OCR Token响应
type OCRTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	Error       string `json:"error,omitempty"`
	ErrorDesc   string `json:"error_description,omitempty"`
}

// OCRRecognizeResponse 百度OCR识别响应
type OCRRecognizeResponse struct {
	LogID       int64 `json:"log_id"`
	WordsResult []struct {
		Words string `json:"words"`
	} `json:"words_result"`
	ErrorCode int    `json:"error_code,omitempty"`
	ErrorMsg  string `json:"error_msg,omitempty"`
}

func NewOCRClient(apiKey, secretKey string, logger *zap.Logger) *OCRClient {
	return &OCRClient{
		apiKey:    apiKey,
		secretKey: secretKey,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		logger: logger,
	}
}

// getAccessToken 获取百度OCR访问令牌（与Node版本对齐）
func (c *OCRClient) getAccessToken() (string, error) {
	c.mutex.RLock()
	// 检查token是否还有效（提前5分钟刷新，与Node逻辑一致）
	if c.accessToken != "" && time.Now().UnixMilli() < c.tokenExpiresAt-300000 {
		token := c.accessToken
		c.mutex.RUnlock()
		return token, nil
	}
	c.mutex.RUnlock()

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 双重检查
	if c.accessToken != "" && time.Now().UnixMilli() < c.tokenExpiresAt-300000 {
		return c.accessToken, nil
	}

	// 请求新token
	tokenURL := "https://aip.baidubce.com/oauth/2.0/token"
	params := url.Values{}
	params.Set("grant_type", "client_credentials")
	params.Set("client_id", c.apiKey)
	params.Set("client_secret", c.secretKey)

	resp, err := c.client.Get(tokenURL + "?" + params.Encode())
	if err != nil {
		c.logger.Error("❌ 百度OCR token请求异常", zap.Error(err))
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var tokenResp OCRTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("Token获取失败: %s", string(body))
	}

	c.accessToken = tokenResp.AccessToken
	expiresIn := tokenResp.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 2592000 // 默认30天，与Node一致
	}
	c.tokenExpiresAt = time.Now().UnixMilli() + int64(expiresIn)*1000

	// 降噪：token 获取成功改为调试级别
	c.logger.Debug("✅ 百度OCR access_token获取成功")
	return c.accessToken, nil
}

// recognizeGeneral 百度通用文字识别标准版（与Node版本对齐）
func (c *OCRClient) recognizeGeneral(base64Image string) (string, error) {
	accessToken, err := c.getAccessToken()
	if err != nil {
		return "", err
	}

	// 清理base64数据（与Node逻辑一致）
	if strings.Contains(base64Image, "base64,") {
		parts := strings.Split(base64Image, "base64,")
		if len(parts) > 1 {
			base64Image = parts[1]
		}
	}

	// 构建请求
	apiURL := fmt.Sprintf("https://aip.baidubce.com/rest/2.0/ocr/v1/general_basic?access_token=%s", accessToken)

	data := url.Values{}
	data.Set("image", base64Image)
	data.Set("language_type", "auto_detect")
	data.Set("detect_direction", "false")
	data.Set("paragraph", "false")
	data.Set("probability", "false")

	resp, err := c.client.PostForm(apiURL, data)
	if err != nil {
		// 降噪：请求异常保留错误级别
		c.logger.Error("❌ 百度OCR标准版请求失败", zap.Error(err))
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result OCRRecognizeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if result.ErrorCode != 0 {
		c.logger.Error("❌ 百度OCR标准版识别失败",
			zap.Int("error_code", result.ErrorCode),
			zap.String("error_msg", result.ErrorMsg))
		return "", fmt.Errorf("[%d] %s", result.ErrorCode, result.ErrorMsg)
	}

	if len(result.WordsResult) > 0 {
		// 拼接所有识别到的文字
		var words []string
		for _, item := range result.WordsResult {
			words = append(words, strings.TrimSpace(item.Words))
		}
		fullText := strings.Join(words, "")

		// 只保留字母和数字（与Node逻辑一致）
		cleanedText := ""
		for _, r := range fullText {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				cleanedText += string(r)
			}
		}

		if cleanedText != "" {
			// 降噪：识别成功改为调试级别
			c.logger.Debug("✅ 百度OCR标准版识别成功",
				zap.String("result", cleanedText),
				zap.String("original", fullText))
			return cleanedText, nil
		}
	}

	// 降噪：保持一次警告
	c.logger.Warn("❌ 百度OCR标准版未识别到有效文字")
	return "", nil
}

// recognizeAccurate 百度高精度文字识别（与Node版本对齐）
func (c *OCRClient) recognizeAccurate(base64Image string) (string, error) {
	accessToken, err := c.getAccessToken()
	if err != nil {
		return "", err
	}

	// 清理base64数据
	if strings.Contains(base64Image, "base64,") {
		parts := strings.Split(base64Image, "base64,")
		if len(parts) > 1 {
			base64Image = parts[1]
		}
	}

	// 构建请求
	apiURL := fmt.Sprintf("https://aip.baidubce.com/rest/2.0/ocr/v1/accurate_basic?access_token=%s", accessToken)

	data := url.Values{}
	data.Set("image", base64Image)
	data.Set("detect_direction", "false")
	data.Set("paragraph", "false")
	data.Set("probability", "false")

	resp, err := c.client.PostForm(apiURL, data)
	if err != nil {
		// 降噪：请求异常保留错误级别
		c.logger.Error("❌ 百度高精度OCR请求失败", zap.Error(err))
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result OCRRecognizeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if result.ErrorCode != 0 {
		c.logger.Error("❌ 百度高精度OCR识别失败",
			zap.Int("error_code", result.ErrorCode),
			zap.String("error_msg", result.ErrorMsg))
		return "", fmt.Errorf("[%d] %s", result.ErrorCode, result.ErrorMsg)
	}

	if len(result.WordsResult) > 0 {
		// 拼接所有识别到的文字
		var words []string
		for _, item := range result.WordsResult {
			words = append(words, strings.TrimSpace(item.Words))
		}
		fullText := strings.Join(words, "")

		// 只保留字母和数字
		cleanedText := ""
		for _, r := range fullText {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				cleanedText += string(r)
			}
		}

		if cleanedText != "" {
			// 降噪：识别成功改为调试级别
			c.logger.Debug("✅ 百度高精度OCR识别成功",
				zap.String("result", cleanedText),
				zap.String("original", fullText))
			return cleanedText, nil
		}
	}

	// 降噪：保持一次警告
	c.logger.Warn("❌ 百度高精度OCR未识别到有效文字")
	return "", nil
}

// RecognizeCaptcha 直接使用高精度识别（验证码专用，与Node版本对齐）
func (c *OCRClient) RecognizeCaptcha(base64Image string) (string, error) {
	// 降噪：识别起始改为调试级别
	c.logger.Debug("🤖 使用高精度OCR识别验证码...")

	// 直接使用高精度版本（与Node逻辑一致）
	result, err := c.recognizeAccurate(base64Image)
	if err != nil {
		c.logger.Error("❌ 验证码识别失败", zap.Error(err))
		return "", err
	}

	if result != "" && len(result) == 4 {
		// 降噪：识别成功改为调试级别
		c.logger.Debug("✅ 验证码识别成功",
			zap.String("result", result),
			zap.Int("length", len(result)))
		return result, nil
	} else if result != "" && len(result) != 4 {
		c.logger.Warn("⚠️ 验证码长度异常",
			zap.String("result", result),
			zap.Int("length", len(result)),
			zap.Int("expected", 4))
		return "", fmt.Errorf("验证码长度异常: %d (期望: 4)", len(result))
	}

	c.logger.Warn("❌ 验证码识别失败")
	return "", fmt.Errorf("验证码识别失败")
}

// RecognizeWithRetry 保留原有的通用识别方法（与Node版本对齐）
func (c *OCRClient) RecognizeWithRetry(base64Image string) (string, error) {
	// 先尝试标准版
	result, err := c.recognizeGeneral(base64Image)
	if err == nil && result != "" && len(result) >= 3 {
		return result, nil
	}

	// 标准版失败，尝试高精度版
	c.logger.Warn("⚠️ 标准版识别效果不佳，尝试高精度版...")
	result, err = c.recognizeAccurate(base64Image)
	if err == nil && result != "" && len(result) >= 3 {
		return result, nil
	}

	c.logger.Error("❌ 百度OCR所有方法都失败了")
	return "", fmt.Errorf("百度OCR所有方法都失败了")
}

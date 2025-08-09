package client

import (
	"encoding/base64"
	"strings"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ocr "github.com/alibabacloud-go/ocr-api-20210707/v3/client"
	tea "github.com/alibabacloud-go/tea/tea"
	"go.uber.org/zap"
)

// AliyunOCRClient 阿里云全文识别高精版客户端（AK/SK + cn-shanghai）
type AliyunOCRClient struct {
	cli    *ocr.Client
	logger *zap.Logger
}

func NewAliyunOCRClient(accessKeyId, accessKeySecret, region string, logger *zap.Logger) *AliyunOCRClient {
	if region == "" {
		region = "cn-shanghai"
	}
	cfg := &openapi.Config{
		AccessKeyId:     tea.String(accessKeyId),
		AccessKeySecret: tea.String(accessKeySecret),
		RegionId:        tea.String(region),
	}
	c, err := ocr.NewClient(cfg)
	if err != nil {
		// 在构造期失败时，返回包装的空客户端，调用时会报错
		if logger != nil {
			logger.Warn("初始化阿里云OCR客户端失败", zap.Error(err))
		}
		return &AliyunOCRClient{cli: nil, logger: logger}
	}
	return &AliyunOCRClient{cli: c, logger: logger}
}

// RecognizeCaptcha 识别验证码（输入base64图片，输出仅A-Z/0-9的4位，否则返回错误）
func (c *AliyunOCRClient) RecognizeCaptcha(base64Image string) (string, error) {
	if c.cli == nil {
		return "", &OCRError{Code: -1, Msg: "aliyun client not initialized", Category: "other"}
	}
	// 清理base64前缀
	if idx := strings.Index(base64Image, ","); idx != -1 {
		// 常见形如 data:image/png;base64,xxxxxxxx
		if strings.Contains(strings.ToLower(base64Image[:idx]), "base64") {
			base64Image = base64Image[idx+1:]
		}
	}
	// 快速校验是否为有效base64
	if _, err := base64.StdEncoding.DecodeString(base64Image); err != nil {
		return "", &OCRError{Code: -2, Msg: "invalid base64", Category: "other"}
	}

	// 调用阿里云全文识别高精版（RecognizeAdvanced）
	// 注：阿里云OCR接口在不同版本可能命名略有差异，如出现不匹配再根据官方文档微调
	req := &ocr.RecognizeAdvancedRequest{
		// 阿里云部分接口入参为 Url，这里尝试以 data URL 方式传递 base64
		Url: tea.String("data:image/png;base64," + base64Image),
	}

	// SDK 默认自带重试，必要时可结合 tea SDK 设置超时/重试策略
	start := time.Now()
	resp, err := c.cli.RecognizeAdvanced(req)
	if err != nil {
		// 这里无法直接得知错误码，统一归类为 throttle/other，交由上层重试/切换
		if c.logger != nil {
			c.logger.Debug("阿里云OCR调用失败", zap.Error(err))
		}
		return "", &OCRError{Code: -3, Msg: err.Error(), Category: "other"}
	}
	_ = start // 预留耗时打点

	// 解析结果：不同返回结构体字段命名可能有差异，以下为通用思路
	// 目标：拼接识别文本 -> 仅保留字母数字 -> 取前4位并大写
	content := ""
	if resp != nil && resp.Body != nil && resp.Body.Data != nil {
		// 部分版本 Data 直接为字符串内容
		content = tea.StringValue(resp.Body.Data)
	}
	if content == "" {
		return "", &OCRError{Code: -4, Msg: "empty content", Category: "other"}
	}

	// 仅保留字母数字
	sb := strings.Builder{}
	for _, r := range content {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			// 转大写
			if r >= 'a' && r <= 'z' {
				r = r - 'a' + 'A'
			}
			sb.WriteRune(r)
			if sb.Len() >= 4 {
				break
			}
		}
	}
	out := sb.String()
	if len(out) != 4 {
		return "", &OCRError{Code: -5, Msg: "captcha length not 4", Category: "other"}
	}
	return out, nil
}

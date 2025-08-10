package client

import "fmt"

// OCRError 统一的OCR错误类型，包含分类信息，便于上层策略处理
// Category 可选："quota"（额度/配额）、"auth"（鉴权/权限）、"throttle"（限流/频率）、"other"
type OCRError struct {
	Code     string
	Msg      string
	Category string
}

func (e *OCRError) Error() string {
	if e == nil {
		return ""
	}
	if e.Category != "" {
		return fmt.Sprintf("[%s:%s] %s", e.Category, e.Code, e.Msg)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Msg)
}

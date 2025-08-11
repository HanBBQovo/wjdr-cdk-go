package client

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"strings"
)

// preprocessCaptchaBase64 对验证码图片进行预处理：去前缀、解码、放大、灰度化、二值化、重编码
func preprocessCaptchaBase64(base64Image string) (string, error) {
	// 1. 去掉 data:image/xxx;base64, 前缀
	normalized := base64Image
	if p := strings.Index(normalized, ","); p > 0 && strings.Contains(strings.ToLower(normalized[:p]), "base64") {
		normalized = normalized[p+1:]
	}
	normalized = strings.ReplaceAll(normalized, "\n", "")
	normalized = strings.ReplaceAll(normalized, "\r", "")
	normalized = strings.TrimSpace(normalized)

	// 2. 解码为图像
	imgBytes, err := base64.StdEncoding.DecodeString(normalized)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}

	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return "", fmt.Errorf("image decode failed: %w", err)
	}

	// 3. 图像预处理：放大 2x + 灰度化 + 二值化
	processed := preprocessImage(img)

	// 4. 编码回 base64
	var buf bytes.Buffer
	if err := png.Encode(&buf, processed); err != nil {
		return "", fmt.Errorf("png encode failed: %w", err)
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// preprocessImage 对图像进行预处理：放大2倍、灰度化、二值化
func preprocessImage(src image.Image) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	// 放大 2 倍
	scaledWidth, scaledHeight := width*2, height*2
	scaled := image.NewRGBA(image.Rect(0, 0, scaledWidth, scaledHeight))

	for y := 0; y < scaledHeight; y++ {
		for x := 0; x < scaledWidth; x++ {
			// 最近邻插值
			srcX := x / 2
			srcY := y / 2
			if srcX >= width {
				srcX = width - 1
			}
			if srcY >= height {
				srcY = height - 1
			}
			scaled.Set(x, y, src.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}

	// 灰度化 + 二值化（阈值 128）
	binary := image.NewGray(image.Rect(0, 0, scaledWidth, scaledHeight))
	for y := 0; y < scaledHeight; y++ {
		for x := 0; x < scaledWidth; x++ {
			r, g, b, _ := scaled.At(x, y).RGBA()
			// 灰度值（加权平均）
			gray := uint8((0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 256)
			// 二值化：大于 128 设为白色，小于等于 128 设为黑色
			if gray > 128 {
				binary.SetGray(x, y, color.Gray{Y: 255})
			} else {
				binary.SetGray(x, y, color.Gray{Y: 0})
			}
		}
	}

	// 可选：轻度高斯模糊去噪（简化版）
	return applyLightSmoothing(binary)
}

// applyLightSmoothing 应用轻度平滑滤波，减少噪点
func applyLightSmoothing(src *image.Gray) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	smoothed := image.NewGray(bounds)

	// 简单的 3x3 平均滤波器（边缘保持原值）
	for y := 1; y < height-1; y++ {
		for x := 1; x < width-1; x++ {
			sum := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					sum += int(src.GrayAt(x+dx, y+dy).Y)
				}
			}
			avg := uint8(sum / 9)
			// 只对中间灰度值进行平滑，保持黑白对比
			if avg > 200 {
				smoothed.SetGray(x, y, color.Gray{Y: 255})
			} else if avg < 55 {
				smoothed.SetGray(x, y, color.Gray{Y: 0})
			} else {
				smoothed.SetGray(x, y, color.Gray{Y: avg})
			}
		}
	}

	// 边缘像素保持原值
	for y := 0; y < height; y++ {
		smoothed.SetGray(0, y, src.GrayAt(0, y))
		smoothed.SetGray(width-1, y, src.GrayAt(width-1, y))
	}
	for x := 0; x < width; x++ {
		smoothed.SetGray(x, 0, src.GrayAt(x, 0))
		smoothed.SetGray(x, height-1, src.GrayAt(x, height-1))
	}

	return smoothed
}

// 计算两个颜色之间的欧几里得距离
func colorDistance(c1, c2 color.Color) float64 {
	r1, g1, b1, _ := c1.RGBA()
	r2, g2, b2, _ := c2.RGBA()
	dr := float64(r1) - float64(r2)
	dg := float64(g1) - float64(g2)
	db := float64(b1) - float64(b2)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}





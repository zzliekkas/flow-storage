package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"

	"image/color"

	"github.com/disintegration/imaging"
)

// 文件处理相关错误
var (
	ErrUnsupportedFileFormat = errors.New("processor: 不支持的文件格式")
	ErrProcessingFailed      = errors.New("processor: 处理文件失败")
	ErrInvalidDimensions     = errors.New("processor: 无效的图片尺寸")
	ErrInvalidOperation      = errors.New("processor: 无效的操作")
)

// ImageFormat 图片格式
type ImageFormat string

// 支持的图片格式
const (
	JPEG ImageFormat = "jpeg"
	PNG  ImageFormat = "png"
	GIF  ImageFormat = "gif"
)

// ProcessorManager 文件处理管理器
type ProcessorManager struct {
	// 存储管理器
	storage *Manager
}

// NewProcessorManager 创建新的文件处理管理器
func NewProcessorManager(manager *Manager) *ProcessorManager {
	return &ProcessorManager{
		storage: manager,
	}
}

// ImageResizeOptions 图片缩放选项
type ImageResizeOptions struct {
	// 目标宽度
	Width int

	// 目标高度
	Height int

	// 质量 (1-100，仅对JPEG有效)
	Quality int

	// 目标格式
	Format ImageFormat

	// 是否保持比例
	KeepAspectRatio bool

	// 调整模式：fit, fill, stretch
	Mode string

	// 填充颜色 (用于填充模式)
	FillColor string

	// 是否保存原始文件
	KeepOriginal bool

	// 目标路径
	TargetPath string

	// 目标磁盘
	TargetDisk string
}

// DefaultImageResizeOptions 返回默认的图片缩放选项
func DefaultImageResizeOptions() *ImageResizeOptions {
	return &ImageResizeOptions{
		Width:           0,
		Height:          0,
		Quality:         85,
		Format:          "",
		KeepAspectRatio: true,
		Mode:            "fit",
		KeepOriginal:    true,
		TargetPath:      "",
		TargetDisk:      "",
	}
}

// ResizeImage 调整图片大小
func (pm *ProcessorManager) ResizeImage(ctx context.Context, sourcePath string, options *ImageResizeOptions) (string, error) {
	// 使用默认配置
	if options == nil {
		options = DefaultImageResizeOptions()
	}

	// 验证尺寸
	if options.Width <= 0 && options.Height <= 0 {
		return "", ErrInvalidDimensions
	}

	// 确定源磁盘和目标磁盘
	sourceDisk, err := pm.storage.DefaultDisk()
	if err != nil {
		return "", err
	}

	targetDisk := sourceDisk
	if options.TargetDisk != "" {
		targetDisk, err = pm.storage.Disk(options.TargetDisk)
		if err != nil {
			return "", err
		}
	}

	// 检查源文件是否存在
	exists, err := sourceDisk.Exists(ctx, sourcePath)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", ErrFileNotFound
	}

	// 读取源文件
	sourceFile, err := sourceDisk.Get(ctx, sourcePath)
	if err != nil {
		return "", err
	}

	sourceData, err := sourceFile.Read(ctx)
	if err != nil {
		return "", err
	}

	// 解码图片
	img, format, err := image.Decode(bytes.NewReader(sourceData))
	if err != nil {
		return "", fmt.Errorf("processor: 解码图片失败: %w", err)
	}

	// 确定目标格式
	targetFormat := options.Format
	if targetFormat == "" {
		// 使用源文件格式
		switch format {
		case "jpeg":
			targetFormat = JPEG
		case "png":
			targetFormat = PNG
		case "gif":
			targetFormat = GIF
		default:
			// 默认JPEG
			targetFormat = JPEG
		}
	}

	// 调整图片大小
	var resized image.Image
	switch options.Mode {
	case "fit":
		resized = pm.fitImage(img, options.Width, options.Height, options.KeepAspectRatio)
	case "fill":
		resized = pm.fillImage(img, options.Width, options.Height)
	case "stretch":
		resized = pm.stretchImage(img, options.Width, options.Height)
	default:
		resized = pm.fitImage(img, options.Width, options.Height, options.KeepAspectRatio)
	}

	// 编码为目标格式
	var buf bytes.Buffer
	switch targetFormat {
	case JPEG:
		err = jpeg.Encode(&buf, resized, &jpeg.Options{Quality: options.Quality})
	case PNG:
		err = png.Encode(&buf, resized)
	case GIF:
		err = gif.Encode(&buf, resized, nil)
	default:
		return "", ErrUnsupportedFileFormat
	}

	if err != nil {
		return "", fmt.Errorf("processor: 编码图片失败: %w", err)
	}

	// 确定目标路径
	targetPath := options.TargetPath
	if targetPath == "" {
		ext := "." + strings.ToLower(string(targetFormat))
		dir := filepath.Dir(sourcePath)
		filename := filepath.Base(sourcePath)
		baseFilename := strings.TrimSuffix(filename, filepath.Ext(filename))

		// 添加尺寸信息到文件名
		sizeInfo := fmt.Sprintf("_%dx%d", options.Width, options.Height)
		targetPath = filepath.Join(dir, baseFilename+sizeInfo+ext)
	}

	// 写入目标文件
	err = targetDisk.Write(ctx, targetPath, buf.Bytes(), WithMimeType(getMimeType(targetFormat)))
	if err != nil {
		return "", err
	}

	return targetPath, nil
}

// WatermarkOptions 水印选项
type WatermarkOptions struct {
	// 水印图片路径
	WatermarkPath string

	// 水印位置: top-left, top-right, bottom-left, bottom-right, center
	Position string

	// 水印透明度 (0-100)
	Opacity int

	// 水印边距
	Margin int

	// 是否保存原始文件
	KeepOriginal bool

	// 目标路径
	TargetPath string

	// 目标磁盘
	TargetDisk string
}

// DefaultWatermarkOptions 返回默认的水印选项
func DefaultWatermarkOptions() *WatermarkOptions {
	return &WatermarkOptions{
		Position:     "bottom-right",
		Opacity:      70,
		Margin:       10,
		KeepOriginal: true,
		TargetPath:   "",
		TargetDisk:   "",
	}
}

// AddWatermark 添加水印
func (pm *ProcessorManager) AddWatermark(ctx context.Context, sourcePath string, options *WatermarkOptions) (string, error) {
	// 使用默认配置
	if options == nil {
		options = DefaultWatermarkOptions()
	}

	// 确定源磁盘和目标磁盘
	sourceDisk, err := pm.storage.DefaultDisk()
	if err != nil {
		return "", err
	}

	targetDisk := sourceDisk
	if options.TargetDisk != "" {
		targetDisk, err = pm.storage.Disk(options.TargetDisk)
		if err != nil {
			return "", err
		}
	}

	// 检查源文件是否存在
	exists, err := sourceDisk.Exists(ctx, sourcePath)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", ErrFileNotFound
	}

	// 检查水印文件是否存在
	exists, err = sourceDisk.Exists(ctx, options.WatermarkPath)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("processor: 水印文件不存在: %s", options.WatermarkPath)
	}

	// 读取源文件
	sourceFile, err := sourceDisk.Get(ctx, sourcePath)
	if err != nil {
		return "", err
	}

	sourceData, err := sourceFile.Read(ctx)
	if err != nil {
		return "", err
	}

	// 解码源图片
	srcImg, format, err := image.Decode(bytes.NewReader(sourceData))
	if err != nil {
		return "", fmt.Errorf("processor: 解码源图片失败: %w", err)
	}

	// 读取水印文件
	watermarkFile, err := sourceDisk.Get(ctx, options.WatermarkPath)
	if err != nil {
		return "", err
	}

	watermarkData, err := watermarkFile.Read(ctx)
	if err != nil {
		return "", err
	}

	// 解码水印图片
	watermarkImg, _, err := image.Decode(bytes.NewReader(watermarkData))
	if err != nil {
		return "", fmt.Errorf("processor: 解码水印图片失败: %w", err)
	}

	// 应用水印
	result := pm.applyWatermark(srcImg, watermarkImg, options.Position, options.Opacity, options.Margin)

	// 编码为目标格式
	var buf bytes.Buffer
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buf, result, &jpeg.Options{Quality: 85})
	case "png":
		err = png.Encode(&buf, result)
	case "gif":
		err = gif.Encode(&buf, result, nil)
	default:
		// 默认JPEG
		err = jpeg.Encode(&buf, result, &jpeg.Options{Quality: 85})
	}

	if err != nil {
		return "", fmt.Errorf("processor: 编码图片失败: %w", err)
	}

	// 确定目标路径
	targetPath := options.TargetPath
	if targetPath == "" {
		ext := filepath.Ext(sourcePath)
		dir := filepath.Dir(sourcePath)
		filename := filepath.Base(sourcePath)
		baseFilename := strings.TrimSuffix(filename, ext)

		// 添加水印标识到文件名
		targetPath = filepath.Join(dir, baseFilename+"_watermarked"+ext)
	}

	// 写入目标文件
	err = targetDisk.Write(ctx, targetPath, buf.Bytes(), WithMimeType(getMimeType(format)))
	if err != nil {
		return "", err
	}

	return targetPath, nil
}

// FileConvertOptions 文件转换选项
type FileConvertOptions struct {
	// 目标格式
	TargetFormat string

	// 转换参数
	Parameters map[string]interface{}

	// 是否保存原始文件
	KeepOriginal bool

	// 目标路径
	TargetPath string

	// 目标磁盘
	TargetDisk string
}

// DefaultFileConvertOptions 返回默认的文件转换选项
func DefaultFileConvertOptions() *FileConvertOptions {
	return &FileConvertOptions{
		Parameters:   make(map[string]interface{}),
		KeepOriginal: true,
		TargetPath:   "",
		TargetDisk:   "",
	}
}

// ConvertFile 转换文件格式
func (pm *ProcessorManager) ConvertFile(ctx context.Context, sourcePath string, options *FileConvertOptions) (string, error) {
	// 目前仅支持图片格式转换
	// 更多格式可以通过整合外部工具如FFmpeg来实现

	if options == nil {
		options = DefaultFileConvertOptions()
	}

	// 验证目标格式
	targetFormat := strings.ToLower(options.TargetFormat)
	var imgFormat ImageFormat
	switch targetFormat {
	case "jpg", "jpeg":
		imgFormat = JPEG
	case "png":
		imgFormat = PNG
	case "gif":
		imgFormat = GIF
	default:
		return "", ErrUnsupportedFileFormat
	}

	// 确定源磁盘和目标磁盘
	sourceDisk, err := pm.storage.DefaultDisk()
	if err != nil {
		return "", err
	}

	targetDisk := sourceDisk
	if options.TargetDisk != "" {
		targetDisk, err = pm.storage.Disk(options.TargetDisk)
		if err != nil {
			return "", err
		}
	}

	// 检查源文件是否存在
	exists, err := sourceDisk.Exists(ctx, sourcePath)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", ErrFileNotFound
	}

	// 读取源文件
	sourceFile, err := sourceDisk.Get(ctx, sourcePath)
	if err != nil {
		return "", err
	}

	sourceData, err := sourceFile.Read(ctx)
	if err != nil {
		return "", err
	}

	// 解码图片
	img, _, err := image.Decode(bytes.NewReader(sourceData))
	if err != nil {
		return "", fmt.Errorf("processor: 解码图片失败: %w", err)
	}

	// 编码为目标格式
	var buf bytes.Buffer

	// 获取质量参数（如果有）
	quality := 85
	if val, ok := options.Parameters["quality"]; ok {
		if q, ok := val.(int); ok {
			quality = q
		}
	}

	switch imgFormat {
	case JPEG:
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	case PNG:
		err = png.Encode(&buf, img)
	case GIF:
		err = gif.Encode(&buf, img, nil)
	}

	if err != nil {
		return "", fmt.Errorf("processor: 编码图片失败: %w", err)
	}

	// 确定目标路径
	targetPath := options.TargetPath
	if targetPath == "" {
		dir := filepath.Dir(sourcePath)
		filename := filepath.Base(sourcePath)
		baseFilename := strings.TrimSuffix(filename, filepath.Ext(filename))
		targetPath = filepath.Join(dir, baseFilename+"."+targetFormat)
	}

	// 写入目标文件
	err = targetDisk.Write(ctx, targetPath, buf.Bytes(), WithMimeType(getMimeType(imgFormat)))
	if err != nil {
		return "", err
	}

	return targetPath, nil
}

// BatchProcessOptions 批量处理选项
type BatchProcessOptions struct {
	// 处理类型: resize, watermark, convert
	ProcessType string

	// 处理选项
	Options interface{}

	// 文件匹配模式
	Pattern string

	// 是否递归处理子目录
	Recursive bool

	// 最大并发处理数
	MaxConcurrent int

	// 结果目录
	ResultDirectory string

	// 目标磁盘
	TargetDisk string
}

// BatchProcessResult 批量处理结果
type BatchProcessResult struct {
	// 成功处理的文件路径
	SuccessFiles []string

	// 处理失败的文件及错误
	FailedFiles map[string]error

	// 处理的总文件数
	TotalCount int

	// 成功处理的文件数
	SuccessCount int

	// 处理失败的文件数
	FailedCount int
}

// DefaultBatchProcessOptions 返回默认的批量处理选项
func DefaultBatchProcessOptions() *BatchProcessOptions {
	return &BatchProcessOptions{
		Pattern:         "*",
		Recursive:       false,
		MaxConcurrent:   4,
		ResultDirectory: "",
		TargetDisk:      "",
	}
}

// BatchProcess 批量处理文件
func (pm *ProcessorManager) BatchProcess(ctx context.Context, directory string, options *BatchProcessOptions) (*BatchProcessResult, error) {
	// TODO: 实现批量处理功能
	// 由于批量处理涉及并发和复杂的逻辑，这里仅提供接口定义

	return nil, errors.New("processor: 批量处理功能尚未实现")
}

// 辅助方法

// fitImage 使图片适应指定尺寸（保持比例）
func (pm *ProcessorManager) fitImage(img image.Image, width, height int, keepAspectRatio bool) image.Image {
	if !keepAspectRatio {
		return imaging.Resize(img, width, height, imaging.Lanczos)
	}

	return imaging.Fit(img, width, height, imaging.Lanczos)
}

// fillImage 填充图片到指定尺寸（保持比例，裁剪超出部分）
func (pm *ProcessorManager) fillImage(img image.Image, width, height int) image.Image {
	return imaging.Fill(img, width, height, imaging.Center, imaging.Lanczos)
}

// stretchImage 拉伸图片到指定尺寸（不保持比例）
func (pm *ProcessorManager) stretchImage(img image.Image, width, height int) image.Image {
	return imaging.Resize(img, width, height, imaging.Lanczos)
}

// applyWatermark 应用水印
func (pm *ProcessorManager) applyWatermark(img image.Image, watermark image.Image, position string, opacity int, margin int) image.Image {
	// 转换为NRGBA格式（支持透明度）
	canvas := imaging.Clone(img)

	// 调整水印透明度
	if opacity < 100 {
		watermark = adjustOpacity(watermark, float64(opacity)/100.0)
	}

	// 计算水印位置
	var x, y int
	imgWidth := canvas.Bounds().Dx()
	imgHeight := canvas.Bounds().Dy()
	watermarkWidth := watermark.Bounds().Dx()
	watermarkHeight := watermark.Bounds().Dy()

	switch position {
	case "top-left":
		x = margin
		y = margin
	case "top-right":
		x = imgWidth - watermarkWidth - margin
		y = margin
	case "bottom-left":
		x = margin
		y = imgHeight - watermarkHeight - margin
	case "bottom-right":
		x = imgWidth - watermarkWidth - margin
		y = imgHeight - watermarkHeight - margin
	case "center":
		x = (imgWidth - watermarkWidth) / 2
		y = (imgHeight - watermarkHeight) / 2
	default:
		// 默认右下角
		x = imgWidth - watermarkWidth - margin
		y = imgHeight - watermarkHeight - margin
	}

	// 应用水印
	return imaging.Overlay(canvas, watermark, image.Pt(x, y), 1.0)
}

// adjustOpacity 调整图片透明度
func adjustOpacity(img image.Image, opacity float64) image.Image {
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			newA := uint16(float64(a) * opacity)
			rgba.Set(x, y, color.RGBA64{
				R: uint16(r),
				G: uint16(g),
				B: uint16(b),
				A: newA,
			})
		}
	}

	return rgba
}

// getMimeType 获取图片格式对应的MIME类型
func getMimeType(format interface{}) string {
	// 支持字符串格式和ImageFormat格式
	switch v := format.(type) {
	case ImageFormat:
		switch v {
		case JPEG:
			return "image/jpeg"
		case PNG:
			return "image/png"
		case GIF:
			return "image/gif"
		default:
			return "application/octet-stream"
		}
	case string:
		switch v {
		case "jpeg", "jpg":
			return "image/jpeg"
		case "png":
			return "image/png"
		case "gif":
			return "image/gif"
		default:
			return "application/octet-stream"
		}
	default:
		return "application/octet-stream"
	}
}

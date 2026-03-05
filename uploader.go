package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// Uploader 文件上传助手
type Uploader struct {
	// manager 文件存储管理器
	manager *Manager

	// disk 要使用的存储驱动名称
	disk string

	// directory 默认上传目录
	directory string

	// allowedMimeTypes 允许的MIME类型
	allowedMimeTypes []string

	// maxFileSize 最大文件大小（字节）
	maxFileSize int64

	// visibility 文件可见性
	visibility string

	// overwrite 是否覆盖同名文件
	overwrite bool

	// generateUniqueName 是否生成唯一文件名
	generateUniqueName bool

	// customNamer 自定义文件命名函数
	customNamer func(filename string) string

	// validators 验证器列表
	validators []FileValidator

	// beforeCallbacks 上传前回调函数
	beforeCallbacks []BeforeUploadFunc

	// afterCallbacks 上传后回调函数
	afterCallbacks []AfterUploadFunc
}

// FileValidator 文件验证器接口
type FileValidator func(ctx context.Context, file *UploadedFile) error

// BeforeUploadFunc 上传前回调函数类型
type BeforeUploadFunc func(ctx context.Context, file *UploadedFile) error

// AfterUploadFunc 上传后回调函数类型
type AfterUploadFunc func(ctx context.Context, file *UploadedFile, metadata *Metadata) error

// UploadedFile 上传的文件信息
type UploadedFile struct {
	// Filename 原始文件名
	Filename string

	// SavedAs 保存后的文件名
	SavedAs string

	// Size 文件大小
	Size int64

	// MimeType MIME类型
	MimeType string

	// Extension 文件扩展名
	Extension string

	// Content 文件内容
	Content []byte

	// Stream 文件数据流
	Stream io.Reader

	// Path 保存路径
	Path string

	// URL 文件URL
	URL string

	// Metadata 文件元数据
	Metadata *Metadata
}

// NewUploader 创建新的文件上传助手
func NewUploader(manager *Manager) *Uploader {
	return &Uploader{
		manager:            manager,
		disk:               "",
		directory:          "uploads",
		allowedMimeTypes:   []string{},
		maxFileSize:        10 * 1024 * 1024, // 默认10MB
		visibility:         "private",
		overwrite:          false,
		generateUniqueName: true,
		validators:         []FileValidator{},
		beforeCallbacks:    []BeforeUploadFunc{},
		afterCallbacks:     []AfterUploadFunc{},
	}
}

// UseDisk 设置使用的存储驱动
func (u *Uploader) UseDisk(disk string) *Uploader {
	u.disk = disk
	return u
}

// InDirectory 设置上传目录
func (u *Uploader) InDirectory(directory string) *Uploader {
	u.directory = directory
	return u
}

// AllowMimeTypes 设置允许的MIME类型
func (u *Uploader) AllowMimeTypes(mimeTypes ...string) *Uploader {
	u.allowedMimeTypes = mimeTypes
	return u
}

// MaxFileSize 设置最大文件大小
func (u *Uploader) MaxFileSize(size int64) *Uploader {
	u.maxFileSize = size
	return u
}

// WithVisibility 设置文件可见性
func (u *Uploader) WithVisibility(visibility string) *Uploader {
	u.visibility = visibility
	return u
}

// AllowOverwrite 设置是否允许覆盖同名文件
func (u *Uploader) AllowOverwrite(allow bool) *Uploader {
	u.overwrite = allow
	return u
}

// GenerateUniqueName 设置是否生成唯一文件名
func (u *Uploader) GenerateUniqueName(generate bool) *Uploader {
	u.generateUniqueName = generate
	return u
}

// WithValidator 添加文件验证器
func (u *Uploader) WithValidator(validator FileValidator) *Uploader {
	u.validators = append(u.validators, validator)
	return u
}

// BeforeUpload 添加上传前回调函数
func (u *Uploader) BeforeUpload(callback BeforeUploadFunc) *Uploader {
	u.beforeCallbacks = append(u.beforeCallbacks, callback)
	return u
}

// AfterUpload 添加上传后回调函数
func (u *Uploader) AfterUpload(callback AfterUploadFunc) *Uploader {
	u.afterCallbacks = append(u.afterCallbacks, callback)
	return u
}

// WithCustomNamer 设置自定义文件命名函数
func (u *Uploader) WithCustomNamer(namer func(filename string) string) *Uploader {
	u.customNamer = namer
	return u
}

// UploadFile 上传文件内容
func (u *Uploader) UploadFile(ctx context.Context, content []byte, filename string) (*UploadedFile, error) {
	// 创建上传的文件信息
	uploadedFile := &UploadedFile{
		Filename:  filename,
		Size:      int64(len(content)),
		Content:   content,
		Extension: strings.TrimPrefix(filepath.Ext(filename), "."),
	}

	// 检测MIME类型
	uploadedFile.MimeType = DetectMimeType(filename, content)

	// 执行验证
	if err := u.validate(ctx, uploadedFile); err != nil {
		return nil, err
	}

	// 执行上传前回调
	for _, callback := range u.beforeCallbacks {
		if err := callback(ctx, uploadedFile); err != nil {
			return nil, err
		}
	}

	// 生成保存的文件名
	savedFilename := u.generateFileName(filename)
	uploadedFile.SavedAs = savedFilename

	// 生成完整的保存路径
	savePath := filepath.Join(u.directory, savedFilename)
	uploadedFile.Path = savePath

	// 获取文件系统
	var fs FileSystem
	var err error
	if u.disk != "" {
		fs, err = u.manager.Disk(u.disk)
	} else {
		fs, err = u.manager.DefaultDisk()
	}
	if err != nil {
		return nil, err
	}

	// 写入选项
	options := []WriteOption{
		WithVisibility(u.visibility),
		WithOverwrite(u.overwrite),
		WithMimeType(uploadedFile.MimeType),
	}

	// 写入文件
	if err := fs.Write(ctx, savePath, content, options...); err != nil {
		return nil, err
	}

	// 创建元数据
	metadata := NewMetadata(savePath).
		WithSize(uploadedFile.Size).
		WithMimeType(uploadedFile.MimeType).
		WithVisibility(u.visibility)

	// 计算校验和
	checksum, _ := CalculateChecksum(content, "md5")
	metadata.WithChecksum(checksum)

	uploadedFile.Metadata = metadata

	// 获取文件URL
	uploadedFile.URL = fs.URL(ctx, savePath)

	// 执行上传后回调
	for _, callback := range u.afterCallbacks {
		if err := callback(ctx, uploadedFile, metadata); err != nil {
			// 尝试删除已上传的文件
			_ = fs.Delete(ctx, savePath)
			return nil, err
		}
	}

	return uploadedFile, nil
}

// UploadFromRequest 从HTTP请求中上传文件
func (u *Uploader) UploadFromRequest(ctx context.Context, r *http.Request, fieldName string) (*UploadedFile, error) {
	// 解析请求
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, err
	}

	// 获取上传的文件
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// 读取文件内容
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	// 调用文件上传方法
	return u.UploadFile(ctx, content, header.Filename)
}

// UploadMultiple 上传多个文件
func (u *Uploader) UploadMultiple(ctx context.Context, files map[string][]byte) ([]*UploadedFile, error) {
	results := make([]*UploadedFile, 0, len(files))
	errors := make([]string, 0)

	for filename, content := range files {
		result, err := u.UploadFile(ctx, content, filename)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %s", filename, err.Error()))
			continue
		}
		results = append(results, result)
	}

	if len(errors) > 0 {
		return results, fmt.Errorf("部分文件上传失败: %s", strings.Join(errors, "; "))
	}

	return results, nil
}

// UploadFromMultipartFile 从multipart.File上传文件
func (u *Uploader) UploadFromMultipartFile(ctx context.Context, file multipart.File, header *multipart.FileHeader) (*UploadedFile, error) {
	// 读取文件内容
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	// 调用文件上传方法
	return u.UploadFile(ctx, content, header.Filename)
}

// validate 验证上传的文件
func (u *Uploader) validate(ctx context.Context, file *UploadedFile) error {
	// 检查文件大小
	if u.maxFileSize > 0 && file.Size > u.maxFileSize {
		return fmt.Errorf("文件大小超过限制: %d 字节 (最大允许 %d 字节)", file.Size, u.maxFileSize)
	}

	// 检查MIME类型
	if len(u.allowedMimeTypes) > 0 {
		allowed := false
		for _, mimeType := range u.allowedMimeTypes {
			if strings.HasSuffix(mimeType, "/*") {
				prefix := strings.TrimSuffix(mimeType, "/*")
				if strings.HasPrefix(file.MimeType, prefix) {
					allowed = true
					break
				}
			} else if mimeType == file.MimeType {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("不支持的文件类型: %s", file.MimeType)
		}
	}

	// 执行自定义验证器
	for _, validator := range u.validators {
		if err := validator(ctx, file); err != nil {
			return err
		}
	}

	return nil
}

// generateFileName 生成保存的文件名
func (u *Uploader) generateFileName(originalName string) string {
	// 如果有自定义命名函数，使用它
	if u.customNamer != nil {
		return u.customNamer(originalName)
	}

	// 如果不需要生成唯一名称，直接返回原始文件名
	if !u.generateUniqueName {
		return originalName
	}

	// 生成唯一文件名
	ext := filepath.Ext(originalName)
	baseName := strings.TrimSuffix(originalName, ext)
	timestamp := time.Now().UnixNano() / int64(time.Millisecond)
	return fmt.Sprintf("%s_%d%s", baseName, timestamp, ext)
}

// ImageValidator 创建图片验证器
func ImageValidator() FileValidator {
	return func(ctx context.Context, file *UploadedFile) error {
		if !strings.HasPrefix(file.MimeType, "image/") {
			return errors.New("文件不是有效的图片")
		}
		return nil
	}
}

// PDFValidator 创建PDF验证器
func PDFValidator() FileValidator {
	return func(ctx context.Context, file *UploadedFile) error {
		if file.MimeType != "application/pdf" {
			return errors.New("文件不是有效的PDF")
		}
		return nil
	}
}

// DocumentValidator 创建文档验证器
func DocumentValidator() FileValidator {
	return func(ctx context.Context, file *UploadedFile) error {
		validTypes := map[string]bool{
			"application/msword": true, // .doc
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true, // .docx
			"application/vnd.ms-excel": true, // .xls
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true, // .xlsx
			"application/vnd.ms-powerpoint":                                             true, // .ppt
			"application/vnd.openxmlformats-officedocument.presentationml.presentation": true, // .pptx
			"text/plain":      true, // .txt
			"application/pdf": true, // .pdf
			"application/rtf": true, // .rtf
		}

		if !validTypes[file.MimeType] {
			return errors.New("文件不是有效的文档格式")
		}
		return nil
	}
}

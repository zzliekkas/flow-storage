package storage

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// Metadata 文件元数据管理
type Metadata struct {
	// Path 文件路径
	Path string `json:"path"`

	// Name 文件名
	Name string `json:"name"`

	// Extension 文件扩展名
	Extension string `json:"extension"`

	// Size 文件大小（字节）
	Size int64 `json:"size"`

	// LastModified 最后修改时间
	LastModified time.Time `json:"last_modified"`

	// MimeType 文件MIME类型
	MimeType string `json:"mime_type"`

	// Visibility 文件可见性
	Visibility string `json:"visibility"`

	// Checksum 文件校验和
	Checksum string `json:"checksum,omitempty"`

	// IsDirectory 是否为目录
	IsDirectory bool `json:"is_directory"`

	// Custom 自定义元数据
	Custom map[string]interface{} `json:"custom,omitempty"`
}

// NewMetadata 创建新的文件元数据对象
func NewMetadata(path string) *Metadata {
	name := filepath.Base(path)
	return &Metadata{
		Path:         path,
		Name:         name,
		Extension:    strings.TrimPrefix(filepath.Ext(name), "."),
		LastModified: time.Now(),
		Visibility:   "private",
		Custom:       make(map[string]interface{}),
	}
}

// WithSize 设置文件大小
func (m *Metadata) WithSize(size int64) *Metadata {
	m.Size = size
	return m
}

// WithLastModified 设置最后修改时间
func (m *Metadata) WithLastModified(lastModified time.Time) *Metadata {
	m.LastModified = lastModified
	return m
}

// WithMimeType 设置MIME类型
func (m *Metadata) WithMimeType(mimeType string) *Metadata {
	m.MimeType = mimeType
	return m
}

// WithVisibility 设置可见性
func (m *Metadata) WithVisibility(visibility string) *Metadata {
	m.Visibility = visibility
	return m
}

// WithChecksum 设置校验和
func (m *Metadata) WithChecksum(checksum string) *Metadata {
	m.Checksum = checksum
	return m
}

// WithIsDirectory 设置是否为目录
func (m *Metadata) WithIsDirectory(isDirectory bool) *Metadata {
	m.IsDirectory = isDirectory
	return m
}

// WithCustom 设置自定义元数据
func (m *Metadata) WithCustom(key string, value interface{}) *Metadata {
	m.Custom[key] = value
	return m
}

// DetectMimeType 尝试检测文件的MIME类型
func DetectMimeType(filename string, data []byte) string {
	// 如果有数据，使用http.DetectContentType
	if len(data) > 0 {
		return http.DetectContentType(data)
	}

	// 根据文件扩展名猜测MIME类型
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".zip":
		return "application/zip"
	case ".mp3":
		return "audio/mpeg"
	case ".mp4":
		return "video/mp4"
	case ".doc", ".docx":
		return "application/msword"
	case ".xls", ".xlsx":
		return "application/vnd.ms-excel"
	case ".ppt", ".pptx":
		return "application/vnd.ms-powerpoint"
	default:
		return "application/octet-stream"
	}
}

// CalculateChecksum 计算数据的校验和
func CalculateChecksum(data []byte, algorithm string) (string, error) {
	switch strings.ToLower(algorithm) {
	case "md5":
		hash := md5.Sum(data)
		return hex.EncodeToString(hash[:]), nil
	default:
		// 默认使用MD5
		hash := md5.Sum(data)
		return hex.EncodeToString(hash[:]), nil
	}
}

// CalculateChecksumFromReader 从数据流计算校验和
func CalculateChecksumFromReader(reader io.Reader, algorithm string) (string, error) {
	hasher := md5.New()
	_, err := io.Copy(hasher, reader)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

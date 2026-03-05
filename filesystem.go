package storage

import (
	"context"
	"io"
	"os"
	"time"
)

// 常见错误定义
var (
	ErrFileNotFound      = os.ErrNotExist
	ErrPermissionDenied  = os.ErrPermission
	ErrInvalidPath       = os.ErrInvalid
	ErrDirectoryNotEmpty = os.ErrExist
	ErrFileAlreadyExists = os.ErrExist
)

// File 表示一个文件对象
type File interface {
	// Path 返回文件的路径
	Path() string

	// Name 返回文件的名称（不包含路径）
	Name() string

	// Extension 返回文件的扩展名
	Extension() string

	// Size 返回文件的大小（字节）
	Size() int64

	// LastModified 返回文件的最后修改时间
	LastModified() time.Time

	// IsDirectory 判断是否为目录
	IsDirectory() bool

	// Read 读取文件内容
	Read(ctx context.Context) ([]byte, error)

	// ReadStream 获取文件的读取流
	ReadStream(ctx context.Context) (io.ReadCloser, error)

	// MimeType 返回文件的MIME类型
	MimeType() string

	// Visibility 返回文件的可见性（public 或 private）
	Visibility() string

	// URL 获取文件的URL（如果适用）
	URL() string

	// TemporaryURL 获取文件的临时URL（带有过期时间）
	TemporaryURL(ctx context.Context, expiration time.Duration) (string, error)

	// Metadata 获取文件的元数据
	Metadata() map[string]interface{}
}

// FileInfo 包含文件的基本信息
type FileInfo struct {
	// 文件路径
	FilePath string `json:"path"`
	// 文件名
	FileName string `json:"name"`
	// 文件大小（字节）
	FileSize int64 `json:"size"`
	// 是否为目录
	IsDir bool `json:"is_directory"`
	// 最后修改时间
	ModTime time.Time `json:"modified_at"`
	// MIME类型
	ContentType string `json:"content_type"`
	// 可见性
	FileVisibility string `json:"visibility"`
	// 元数据
	MetaData map[string]interface{} `json:"metadata,omitempty"`
}

// FileSystem 文件存储系统接口
type FileSystem interface {
	// Get 获取指定路径的文件
	Get(ctx context.Context, path string) (File, error)

	// Exists 检查文件是否存在
	Exists(ctx context.Context, path string) (bool, error)

	// Write 写入文件内容
	Write(ctx context.Context, path string, content []byte, options ...WriteOption) error

	// WriteStream 通过流写入文件
	WriteStream(ctx context.Context, path string, content io.Reader, options ...WriteOption) error

	// Delete 删除文件
	Delete(ctx context.Context, path string) error

	// DeleteDirectory 删除目录及其内容
	DeleteDirectory(ctx context.Context, path string) error

	// CreateDirectory 创建目录
	CreateDirectory(ctx context.Context, path string, options ...WriteOption) error

	// Files 列出目录下的所有文件
	Files(ctx context.Context, directory string) ([]File, error)

	// AllFiles 递归列出目录下的所有文件
	AllFiles(ctx context.Context, directory string) ([]File, error)

	// Directories 列出目录下的所有子目录
	Directories(ctx context.Context, directory string) ([]string, error)

	// AllDirectories 递归列出目录下的所有子目录
	AllDirectories(ctx context.Context, directory string) ([]string, error)

	// Copy 复制文件
	Copy(ctx context.Context, source, destination string) error

	// Move 移动文件
	Move(ctx context.Context, source, destination string) error

	// Size 获取文件大小
	Size(ctx context.Context, path string) (int64, error)

	// LastModified 获取文件修改时间
	LastModified(ctx context.Context, path string) (time.Time, error)

	// MimeType 获取文件MIME类型
	MimeType(ctx context.Context, path string) (string, error)

	// SetVisibility 设置文件可见性
	SetVisibility(ctx context.Context, path, visibility string) error

	// Visibility 获取文件可见性
	Visibility(ctx context.Context, path string) (string, error)

	// URL 获取文件URL
	URL(ctx context.Context, path string) string

	// TemporaryURL 获取临时URL
	TemporaryURL(ctx context.Context, path string, expiration time.Duration) (string, error)

	// Checksum 计算文件校验和
	Checksum(ctx context.Context, path string, algorithm string) (string, error)
}

// WriteOption 写入文件的选项
type WriteOption func(*WriteOptions)

// WriteOptions 写入文件的选项集合
type WriteOptions struct {
	// 文件可见性：public 或 private
	Visibility string

	// 文件元数据
	Metadata map[string]interface{}

	// 文件权限
	Permissions os.FileMode

	// MIME类型
	MimeType string

	// 是否覆盖已存在的文件
	Overwrite bool
}

// DefaultWriteOptions 返回默认的写入选项
func DefaultWriteOptions() *WriteOptions {
	return &WriteOptions{
		Visibility:  "private",
		Metadata:    make(map[string]interface{}),
		Permissions: 0644,
		Overwrite:   false,
	}
}

// WithVisibility 设置文件可见性选项
func WithVisibility(visibility string) WriteOption {
	return func(o *WriteOptions) {
		o.Visibility = visibility
	}
}

// WithMetadata 设置文件元数据选项
func WithMetadata(metadata map[string]interface{}) WriteOption {
	return func(o *WriteOptions) {
		o.Metadata = metadata
	}
}

// WithPermissions 设置文件权限选项
func WithPermissions(permissions os.FileMode) WriteOption {
	return func(o *WriteOptions) {
		o.Permissions = permissions
	}
}

// WithMimeType 设置文件MIME类型选项
func WithMimeType(mimeType string) WriteOption {
	return func(o *WriteOptions) {
		o.MimeType = mimeType
	}
}

// WithOverwrite 设置是否覆盖已存在文件选项
func WithOverwrite(overwrite bool) WriteOption {
	return func(o *WriteOptions) {
		o.Overwrite = overwrite
	}
}

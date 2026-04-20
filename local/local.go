package local

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zzliekkas/flow-storage/v3/core"
)

// LocalFile 表示本地文件系统中的文件
type LocalFile struct {
	// path 文件完整路径
	path string

	// root 根目录
	root string

	// info 文件信息
	info os.FileInfo

	// metadata 文件元数据
	metadata *core.Metadata

	// visibility 文件可见性
	visibility string

	// baseURL 基础URL
	baseURL string
}

// Path 返回文件路径
func (f *LocalFile) Path() string {
	relPath, _ := filepath.Rel(f.root, f.path)
	return relPath
}

// Name 返回文件名
func (f *LocalFile) Name() string {
	return filepath.Base(f.path)
}

// Extension 返回文件扩展名
func (f *LocalFile) Extension() string {
	return strings.TrimPrefix(filepath.Ext(f.path), ".")
}

// Size 返回文件大小
func (f *LocalFile) Size() int64 {
	if f.info == nil {
		return 0
	}
	return f.info.Size()
}

// LastModified 返回最后修改时间
func (f *LocalFile) LastModified() time.Time {
	if f.info == nil {
		return time.Time{}
	}
	return f.info.ModTime()
}

// IsDirectory 判断是否为目录
func (f *LocalFile) IsDirectory() bool {
	if f.info == nil {
		return false
	}
	return f.info.IsDir()
}

// Read 读取文件内容
func (f *LocalFile) Read(ctx context.Context) ([]byte, error) {
	return os.ReadFile(f.path)
}

// ReadStream 获取文件的读取流
func (f *LocalFile) ReadStream(ctx context.Context) (io.ReadCloser, error) {
	return os.Open(f.path)
}

// MimeType 返回文件MIME类型
func (f *LocalFile) MimeType() string {
	if f.metadata != nil && f.metadata.MimeType != "" {
		return f.metadata.MimeType
	}

	// 尝试通过扩展名判断MIME类型
	ext := f.Extension()
	if contentType := getContentTypeByExtension(ext); contentType != "" {
		return contentType
	}

	// 读取文件前512字节以检测类型
	file, err := os.Open(f.path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return "application/octet-stream"
	}

	return http.DetectContentType(buffer[:n])
}

// Visibility 返回文件可见性
func (f *LocalFile) Visibility() string {
	return f.visibility
}

// URL 获取文件URL
func (f *LocalFile) URL() string {
	if f.baseURL == "" {
		return ""
	}

	relPath, err := filepath.Rel(f.root, f.path)
	if err != nil {
		return ""
	}

	// 替换Windows路径分隔符
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(f.baseURL, "/"), relPath)
}

// TemporaryURL 获取临时URL
func (f *LocalFile) TemporaryURL(ctx context.Context, expiration time.Duration) (string, error) {
	// 本地文件系统不支持临时URL
	return f.URL(), nil
}

// Metadata 获取文件元数据
func (f *LocalFile) Metadata() map[string]interface{} {
	if f.metadata == nil || f.metadata.Custom == nil {
		return make(map[string]interface{})
	}
	return f.metadata.Custom
}

// LocalFileSystem 本地文件系统驱动
type LocalFileSystem struct {
	// root 根目录
	root string

	// baseURL 基础URL
	baseURL string

	// permissions 默认权限
	permissions struct {
		file      os.FileMode
		directory os.FileMode
	}

	// visibility 映射
	visibility struct {
		public  os.FileMode
		private os.FileMode
	}
}

// LocalConfig 本地文件系统配置
type LocalConfig struct {
	// Root 根目录
	Root string

	// BaseURL 基础URL
	BaseURL string

	// FilePermissions 文件权限
	FilePermissions os.FileMode

	// DirectoryPermissions 目录权限
	DirectoryPermissions os.FileMode

	// PublicPermissions 公共文件权限
	PublicPermissions os.FileMode

	// PrivatePermissions 私有文件权限
	PrivatePermissions os.FileMode
}

// DefaultConfig 返回默认配置
func DefaultConfig() LocalConfig {
	return LocalConfig{
		Root:                 "./storage",
		BaseURL:              "/storage",
		FilePermissions:      0644,
		DirectoryPermissions: 0755,
		PublicPermissions:    0644,
		PrivatePermissions:   0600,
	}
}

// New 创建新的本地文件系统驱动
func New(config LocalConfig) (*LocalFileSystem, error) {
	// 确保根目录存在
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(root); os.IsNotExist(err) {
		if err := os.MkdirAll(root, config.DirectoryPermissions); err != nil {
			return nil, err
		}
	}

	fs := &LocalFileSystem{
		root:    root,
		baseURL: config.BaseURL,
	}

	fs.permissions.file = config.FilePermissions
	fs.permissions.directory = config.DirectoryPermissions
	fs.visibility.public = config.PublicPermissions
	fs.visibility.private = config.PrivatePermissions

	return fs, nil
}

// Get 获取指定路径的文件
func (fs *LocalFileSystem) Get(ctx context.Context, path string) (core.File, error) {
	fullPath := fs.fullPath(path)

	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	visibility, _ := fs.getVisibility(fullPath)

	return &LocalFile{
		path:       fullPath,
		root:       fs.root,
		info:       info,
		visibility: visibility,
		baseURL:    fs.baseURL,
	}, nil
}

// Exists 检查文件是否存在
func (fs *LocalFileSystem) Exists(ctx context.Context, path string) (bool, error) {
	fullPath := fs.fullPath(path)
	_, err := os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Write 写入文件内容
func (fs *LocalFileSystem) Write(ctx context.Context, path string, content []byte, options ...core.WriteOption) error {
	fullPath := fs.fullPath(path)

	// 应用选项
	opts := core.DefaultWriteOptions()
	for _, option := range options {
		option(opts)
	}

	// 确保目录存在
	dir := filepath.Dir(fullPath)
	if err := fs.ensureDirectory(dir, fs.permissions.directory); err != nil {
		return err
	}

	// 检查文件是否已存在
	if _, err := os.Stat(fullPath); err == nil && !opts.Overwrite {
		return core.ErrFileAlreadyExists
	}

	// 写入文件
	if err := os.WriteFile(fullPath, content, opts.Permissions); err != nil {
		return err
	}

	// 设置权限
	var mode os.FileMode
	if opts.Visibility == "public" {
		mode = fs.visibility.public
	} else {
		mode = fs.visibility.private
	}
	return os.Chmod(fullPath, mode)
}

// WriteStream 通过流写入文件
func (fs *LocalFileSystem) WriteStream(ctx context.Context, path string, content io.Reader, options ...core.WriteOption) error {
	// 读取内容
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}

	// 使用Write方法
	return fs.Write(ctx, path, data, options...)
}

// Delete 删除文件
func (fs *LocalFileSystem) Delete(ctx context.Context, path string) error {
	fullPath := fs.fullPath(path)

	// 检查是否为目录
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在视为删除成功
		}
		return err
	}

	if info.IsDir() {
		return fmt.Errorf("删除失败: '%s' 是一个目录，请使用DeleteDirectory", path)
	}

	return os.Remove(fullPath)
}

// DeleteDirectory 删除目录及其内容
func (fs *LocalFileSystem) DeleteDirectory(ctx context.Context, path string) error {
	fullPath := fs.fullPath(path)

	// 检查是否为目录
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 目录不存在视为删除成功
		}
		return err
	}

	if !info.IsDir() {
		return fmt.Errorf("删除失败: '%s' 不是一个目录", path)
	}

	return os.RemoveAll(fullPath)
}

// CreateDirectory 创建目录
func (fs *LocalFileSystem) CreateDirectory(ctx context.Context, path string, options ...core.WriteOption) error {
	fullPath := fs.fullPath(path)

	// 应用选项
	opts := core.DefaultWriteOptions()
	for _, option := range options {
		option(opts)
	}

	// 设置权限
	var mode os.FileMode
	if opts.Visibility == "public" {
		mode = fs.visibility.public
	} else {
		mode = fs.visibility.private
	}

	return fs.ensureDirectory(fullPath, mode)
}

// Files 列出目录下的所有文件
func (fs *LocalFileSystem) Files(ctx context.Context, directory string) ([]core.File, error) {
	fullPath := fs.fullPath(directory)

	// 检查目录是否存在
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("'%s' 不是一个目录", directory)
	}

	// 读取目录内容
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	// 过滤出文件
	var files []core.File
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		entryPath := filepath.Join(directory, entry.Name())
		file, err := fs.Get(ctx, entryPath)
		if err != nil {
			continue // 跳过无法访问的文件
		}

		files = append(files, file)
	}

	return files, nil
}

// AllFiles 递归列出目录下的所有文件
func (fs *LocalFileSystem) AllFiles(ctx context.Context, directory string) ([]core.File, error) {
	fullPath := fs.fullPath(directory)

	// 检查目录是否存在
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("'%s' 不是一个目录", directory)
	}

	var files []core.File
	err = filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(fs.root, path)
		if err != nil {
			return err
		}

		file, err := fs.Get(ctx, relPath)
		if err != nil {
			return nil // 跳过无法访问的文件
		}

		files = append(files, file)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// Directories 列出目录下的所有子目录
func (fs *LocalFileSystem) Directories(ctx context.Context, directory string) ([]string, error) {
	fullPath := fs.fullPath(directory)

	// 检查目录是否存在
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("'%s' 不是一个目录", directory)
	}

	// 读取目录内容
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	// 过滤出子目录
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		entryPath := filepath.Join(directory, entry.Name())
		dirs = append(dirs, entryPath)
	}

	return dirs, nil
}

// AllDirectories 递归列出目录下的所有子目录
func (fs *LocalFileSystem) AllDirectories(ctx context.Context, directory string) ([]string, error) {
	fullPath := fs.fullPath(directory)

	// 检查目录是否存在
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("'%s' 不是一个目录", directory)
	}

	var dirs []string
	err = filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		if path == fullPath {
			return nil // 跳过根目录
		}

		relPath, err := filepath.Rel(fs.root, path)
		if err != nil {
			return err
		}

		dirs = append(dirs, relPath)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return dirs, nil
}

// Copy 复制文件
func (fs *LocalFileSystem) Copy(ctx context.Context, source, destination string) error {
	sourceFullPath := fs.fullPath(source)
	destFullPath := fs.fullPath(destination)

	// 检查源文件是否存在
	sourceInfo, err := os.Stat(sourceFullPath)
	if err != nil {
		return err
	}
	if sourceInfo.IsDir() {
		return fmt.Errorf("复制失败: '%s' 是一个目录", source)
	}

	// 确保目标目录存在
	destDir := filepath.Dir(destFullPath)
	if err := fs.ensureDirectory(destDir, fs.permissions.directory); err != nil {
		return err
	}

	// 读取源文件内容
	content, err := os.ReadFile(sourceFullPath)
	if err != nil {
		return err
	}

	// 写入目标文件
	if err := os.WriteFile(destFullPath, content, sourceInfo.Mode()); err != nil {
		return err
	}

	return nil
}

// Move 移动文件
func (fs *LocalFileSystem) Move(ctx context.Context, source, destination string) error {
	// 先复制，再删除
	if err := fs.Copy(ctx, source, destination); err != nil {
		return err
	}
	return fs.Delete(ctx, source)
}

// Size 获取文件大小
func (fs *LocalFileSystem) Size(ctx context.Context, path string) (int64, error) {
	fullPath := fs.fullPath(path)

	info, err := os.Stat(fullPath)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("'%s' 是一个目录", path)
	}

	return info.Size(), nil
}

// LastModified 获取文件修改时间
func (fs *LocalFileSystem) LastModified(ctx context.Context, path string) (time.Time, error) {
	fullPath := fs.fullPath(path)

	info, err := os.Stat(fullPath)
	if err != nil {
		return time.Time{}, err
	}

	return info.ModTime(), nil
}

// MimeType 获取文件MIME类型
func (fs *LocalFileSystem) MimeType(ctx context.Context, path string) (string, error) {
	file, err := fs.Get(ctx, path)
	if err != nil {
		return "", err
	}

	return file.MimeType(), nil
}

// SetVisibility 设置文件可见性
func (fs *LocalFileSystem) SetVisibility(ctx context.Context, path string, visibility string) error {
	fullPath := fs.fullPath(path)

	var mode os.FileMode
	if visibility == "public" {
		mode = fs.visibility.public
	} else {
		mode = fs.visibility.private
	}

	return os.Chmod(fullPath, mode)
}

// Visibility 获取文件可见性
func (fs *LocalFileSystem) Visibility(ctx context.Context, path string) (string, error) {
	return fs.getVisibility(fs.fullPath(path))
}

// URL 获取文件URL
func (fs *LocalFileSystem) URL(ctx context.Context, path string) string {
	if fs.baseURL == "" {
		return ""
	}

	// 替换Windows路径分隔符
	path = strings.ReplaceAll(path, "\\", "/")
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(fs.baseURL, "/"), strings.TrimPrefix(path, "/"))
}

// TemporaryURL 获取临时URL
func (fs *LocalFileSystem) TemporaryURL(ctx context.Context, path string, expiration time.Duration) (string, error) {
	// 本地文件系统不支持临时URL
	return fs.URL(ctx, path), nil
}

// Checksum 计算文件校验和
func (fs *LocalFileSystem) Checksum(ctx context.Context, path string, algorithm string) (string, error) {
	fullPath := fs.fullPath(path)

	// 读取文件内容
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}

	return core.CalculateChecksum(content, algorithm)
}

// fullPath 获取完整文件路径
func (fs *LocalFileSystem) fullPath(path string) string {
	return filepath.Join(fs.root, path)
}

// ensureDirectory 确保目录存在
func (fs *LocalFileSystem) ensureDirectory(directory string, mode os.FileMode) error {
	// 检查目录是否存在
	info, err := os.Stat(directory)
	if err == nil {
		// 目录存在，检查是否为目录
		if !info.IsDir() {
			return fmt.Errorf("'%s' 已存在且不是一个目录", directory)
		}
		return nil
	}

	// 创建目录
	if os.IsNotExist(err) {
		return os.MkdirAll(directory, mode)
	}

	return err
}

// getVisibility 获取文件可见性
func (fs *LocalFileSystem) getVisibility(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	mode := info.Mode().Perm()
	if mode&0004 != 0 { // 检查other是否有读权限
		return "public", nil
	}
	return "private", nil
}

// getContentTypeByExtension 通过扩展名猜测MIME类型
func getContentTypeByExtension(ext string) string {
	return core.DetectMimeType("file."+ext, nil)
}

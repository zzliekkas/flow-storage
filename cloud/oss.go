package cloud

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/zzliekkas/flow-storage/v3"
)

// OSSConfig 阿里云OSS配置选项
type OSSConfig struct {
	// Endpoint 端点URL
	Endpoint string

	// AccessKeyID 访问密钥ID
	AccessKeyID string

	// AccessKeySecret 访问密钥
	AccessKeySecret string

	// Bucket 存储桶名称
	Bucket string

	// PublicURL 公共URL前缀
	PublicURL string

	// UseSSL 是否使用SSL
	UseSSL bool

	// DefaultVisibility 默认可见性
	DefaultVisibility string

	// ConnectTimeout 连接超时
	ConnectTimeout time.Duration

	// ReadWriteTimeout 读写超时
	ReadWriteTimeout time.Duration

	// EnableCRC 是否启用CRC校验
	EnableCRC bool
}

// DefaultOSSConfig 返回默认OSS配置
func DefaultOSSConfig() OSSConfig {
	return OSSConfig{
		UseSSL:            true,
		DefaultVisibility: "private",
		ConnectTimeout:    30 * time.Second,
		ReadWriteTimeout:  60 * time.Second,
		EnableCRC:         true,
	}
}

// OSSFile 表示OSS上的文件
type OSSFile struct {
	path        string
	name        string
	size        int64
	modTime     time.Time
	isDir       bool
	contentType string
	visibility  string
	bucket      string
	publicURL   string
	metadata    map[string]interface{}
}

// Path 实现storage.File接口
func (f *OSSFile) Path() string {
	return f.path
}

// Name 实现storage.File接口
func (f *OSSFile) Name() string {
	return f.name
}

// Extension 实现storage.File接口
func (f *OSSFile) Extension() string {
	return filepath.Ext(f.name)
}

// Size 实现storage.File接口
func (f *OSSFile) Size() int64 {
	return f.size
}

// LastModified 实现storage.File接口
func (f *OSSFile) LastModified() time.Time {
	return f.modTime
}

// IsDirectory 实现storage.File接口
func (f *OSSFile) IsDirectory() bool {
	return f.isDir
}

// Read 实现storage.File接口
func (f *OSSFile) Read(ctx context.Context) ([]byte, error) {
	if f.isDir {
		return nil, fmt.Errorf("cannot read directory: %s", f.path)
	}

	// 由于OSSFile不持有client引用，这个方法不完整
	// 实际实现应在OSSFileSystem中，并传递reader给此方法
	return nil, fmt.Errorf("oss file read not implemented directly, use filesystem instead")
}

// ReadStream 实现storage.File接口
func (f *OSSFile) ReadStream(ctx context.Context) (io.ReadCloser, error) {
	if f.isDir {
		return nil, fmt.Errorf("cannot read directory: %s", f.path)
	}

	// 同上
	return nil, fmt.Errorf("oss file read stream not implemented directly, use filesystem instead")
}

// MimeType 实现storage.File接口
func (f *OSSFile) MimeType() string {
	return f.contentType
}

// Visibility 实现storage.File接口
func (f *OSSFile) Visibility() string {
	return f.visibility
}

// URL 实现storage.File接口
func (f *OSSFile) URL() string {
	if f.publicURL != "" && f.visibility == "public" {
		if strings.HasSuffix(f.publicURL, "/") {
			return f.publicURL + f.path
		}
		return f.publicURL + "/" + f.path
	}
	return ""
}

// TemporaryURL 实现storage.File接口
func (f *OSSFile) TemporaryURL(ctx context.Context, expiration time.Duration) (string, error) {
	// 同上，需要在OSSFileSystem中实现
	return "", fmt.Errorf("oss temporary url not implemented directly, use filesystem instead")
}

// Metadata 实现storage.File接口
func (f *OSSFile) Metadata() map[string]interface{} {
	return f.metadata
}

// OSSFileSystem 实现基于OSS的文件系统
type OSSFileSystem struct {
	client     *oss.Client
	bucket     *oss.Bucket
	config     OSSConfig
	bucketName string
}

// New 创建新的OSS文件系统
func NewOSS(cfg OSSConfig) (*OSSFileSystem, error) {
	// 创建OSS客户端
	options := []oss.ClientOption{
		oss.UseCname(false),
		oss.SecurityToken(""),
		oss.EnableCRC(cfg.EnableCRC),
	}

	// 添加超时选项
	if cfg.ConnectTimeout > 0 || cfg.ReadWriteTimeout > 0 {
		options = append(options, oss.Timeout(int64(cfg.ConnectTimeout/time.Second), int64(cfg.ReadWriteTimeout/time.Second)))
	}

	client, err := oss.New(
		cfg.Endpoint,
		cfg.AccessKeyID,
		cfg.AccessKeySecret,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create oss client: %w", err)
	}

	// 获取存储桶
	bucket, err := client.Bucket(cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket: %w", err)
	}

	return &OSSFileSystem{
		client:     client,
		bucket:     bucket,
		config:     cfg,
		bucketName: cfg.Bucket,
	}, nil
}

// Get 实现storage.FileSystem接口
func (fs *OSSFileSystem) Get(ctx context.Context, path string) (storage.File, error) {
	path = normalizePath(path)

	// 如果路径以/结尾，认为是目录
	if strings.HasSuffix(path, "/") {
		return &OSSFile{
			path:       path,
			name:       filepath.Base(strings.TrimSuffix(path, "/")),
			isDir:      true,
			bucket:     fs.bucketName,
			visibility: fs.config.DefaultVisibility,
			publicURL:  fs.config.PublicURL,
		}, nil
	}

	// 获取对象元数据
	header, err := fs.bucket.GetObjectMeta(path)
	if err != nil {
		return nil, mapOSSError(err)
	}

	// 提取文件大小
	size := int64(0)
	if sizeStr := header.Get("Content-Length"); sizeStr != "" {
		fmt.Sscanf(sizeStr, "%d", &size)
	}

	// 提取最后修改时间
	modTime := time.Time{}
	if lastModStr := header.Get("Last-Modified"); lastModStr != "" {
		modTime, _ = time.Parse(http.TimeFormat, lastModStr)
	}

	// 提取内容类型
	contentType := header.Get("Content-Type")

	// 提取可见性（不直接支持，通过ACL判断）
	visibility := fs.config.DefaultVisibility
	acl, err := fs.bucket.GetObjectACL(path)
	if err == nil {
		if acl.ACL == "public-read" || acl.ACL == "public-read-write" {
			visibility = "public"
		}
	}

	// 提取元数据
	metadata := make(map[string]interface{})
	for k, v := range header {
		if strings.HasPrefix(k, "X-Oss-Meta-") {
			metaKey := strings.TrimPrefix(k, "X-Oss-Meta-")
			if len(v) > 0 {
				metadata[metaKey] = v[0]
			}
		}
	}

	file := &OSSFile{
		path:        path,
		name:        filepath.Base(path),
		size:        size,
		modTime:     modTime,
		isDir:       false,
		contentType: contentType,
		visibility:  visibility,
		bucket:      fs.bucketName,
		publicURL:   fs.config.PublicURL,
		metadata:    metadata,
	}

	return file, nil
}

// Exists 实现storage.FileSystem接口
func (fs *OSSFileSystem) Exists(ctx context.Context, path string) (bool, error) {
	path = normalizePath(path)
	exist, err := fs.bucket.IsObjectExist(path)
	if err != nil {
		return false, fmt.Errorf("failed to check object existence: %w", err)
	}
	return exist, nil
}

// Write 实现storage.FileSystem接口
func (fs *OSSFileSystem) Write(ctx context.Context, path string, content []byte, options ...storage.WriteOption) error {
	path = normalizePath(path)

	// 应用写入选项
	writeOptions := storage.DefaultWriteOptions()
	for _, option := range options {
		option(writeOptions)
	}

	// 如果不允许覆盖，检查文件是否已存在
	if !writeOptions.Overwrite {
		exists, err := fs.Exists(ctx, path)
		if err != nil {
			return err
		}
		if exists {
			return storage.ErrFileAlreadyExists
		}
	}

	// 准备OSS选项
	ossOptions := []oss.Option{}

	// 设置内容类型
	contentType := writeOptions.MimeType
	if contentType == "" {
		contentType = detectContentType(content, path)
	}
	ossOptions = append(ossOptions, oss.ContentType(contentType))

	// 设置元数据
	for k, v := range writeOptions.Metadata {
		if str, ok := v.(string); ok {
			ossOptions = append(ossOptions, oss.Meta(k, str))
		} else {
			ossOptions = append(ossOptions, oss.Meta(k, fmt.Sprintf("%v", v)))
		}
	}

	// 上传内容
	err := fs.bucket.PutObject(path, bytes.NewReader(content), ossOptions...)
	if err != nil {
		return fmt.Errorf("failed to upload object: %w", err)
	}

	// 设置可见性
	if writeOptions.Visibility == "public" {
		err = fs.bucket.SetObjectACL(path, oss.ACLPublicRead)
	} else {
		err = fs.bucket.SetObjectACL(path, oss.ACLPrivate)
	}
	if err != nil {
		return fmt.Errorf("failed to set object ACL: %w", err)
	}

	return nil
}

// WriteStream 实现storage.FileSystem接口
func (fs *OSSFileSystem) WriteStream(ctx context.Context, path string, content io.Reader, options ...storage.WriteOption) error {
	// 读取整个流
	data, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("failed to read content stream: %w", err)
	}

	// 调用Write方法
	return fs.Write(ctx, path, data, options...)
}

// Delete 实现storage.FileSystem接口
func (fs *OSSFileSystem) Delete(ctx context.Context, path string) error {
	path = normalizePath(path)

	// 检查文件是否存在
	exists, err := fs.Exists(ctx, path)
	if err != nil {
		return err
	}
	if !exists {
		return storage.ErrFileNotFound
	}

	// 删除对象
	err = fs.bucket.DeleteObject(path)
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	return nil
}

// DeleteDirectory 实现storage.FileSystem接口
func (fs *OSSFileSystem) DeleteDirectory(ctx context.Context, path string) error {
	path = normalizePath(path)
	if path != "" && !strings.HasSuffix(path, "/") {
		path += "/"
	}

	// 列出目录下的所有对象
	marker := ""
	for {
		lsRes, err := fs.bucket.ListObjects(oss.Prefix(path), oss.Marker(marker))
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		// 没有更多对象
		if len(lsRes.Objects) == 0 {
			break
		}

		// 构建要删除的对象列表
		objects := make([]string, 0, len(lsRes.Objects))
		for _, object := range lsRes.Objects {
			objects = append(objects, object.Key)
		}

		// 批量删除对象
		_, err = fs.bucket.DeleteObjects(objects)
		if err != nil {
			return fmt.Errorf("failed to delete objects: %w", err)
		}

		// 检查是否还有更多对象
		if lsRes.IsTruncated {
			marker = lsRes.NextMarker
		} else {
			break
		}
	}

	return nil
}

// CreateDirectory 实现storage.FileSystem接口
func (fs *OSSFileSystem) CreateDirectory(ctx context.Context, path string, options ...storage.WriteOption) error {
	path = normalizePath(path)
	if path == "" {
		return nil // 根目录已存在
	}

	if !strings.HasSuffix(path, "/") {
		path += "/"
	}

	// 应用写入选项
	writeOptions := storage.DefaultWriteOptions()
	for _, option := range options {
		option(writeOptions)
	}

	// 准备OSS选项
	ossOptions := []oss.Option{
		oss.ContentType("application/x-directory"),
	}

	// 创建一个空对象作为目录标记
	err := fs.bucket.PutObject(path, bytes.NewReader([]byte{}), ossOptions...)
	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 设置可见性
	if writeOptions.Visibility == "public" {
		err = fs.bucket.SetObjectACL(path, oss.ACLPublicRead)
	} else {
		err = fs.bucket.SetObjectACL(path, oss.ACLPrivate)
	}
	if err != nil {
		return fmt.Errorf("failed to set directory ACL: %w", err)
	}

	return nil
}

// Files 实现storage.FileSystem接口
func (fs *OSSFileSystem) Files(ctx context.Context, directory string) ([]storage.File, error) {
	directory = normalizePath(directory)
	if directory != "" && !strings.HasSuffix(directory, "/") {
		directory += "/"
	}

	// 列出目录下的对象
	lsRes, err := fs.bucket.ListObjects(oss.Prefix(directory), oss.Delimiter("/"))
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	result := make([]storage.File, 0, len(lsRes.Objects))
	for _, obj := range lsRes.Objects {
		// 跳过目录标记
		if obj.Key == directory || strings.HasSuffix(obj.Key, "/") {
			continue
		}

		visibility := "private"
		acl, err := fs.bucket.GetObjectACL(obj.Key)
		if err == nil && (acl.ACL == "public-read" || acl.ACL == "public-read-write") {
			visibility = "public"
		}

		file := &OSSFile{
			path:       obj.Key,
			name:       filepath.Base(obj.Key),
			size:       obj.Size,
			modTime:    obj.LastModified,
			isDir:      false,
			visibility: visibility,
			bucket:     fs.bucketName,
			publicURL:  fs.config.PublicURL,
		}

		result = append(result, file)
	}

	return result, nil
}

// AllFiles 实现storage.FileSystem接口
func (fs *OSSFileSystem) AllFiles(ctx context.Context, directory string) ([]storage.File, error) {
	directory = normalizePath(directory)
	if directory != "" && !strings.HasSuffix(directory, "/") {
		directory += "/"
	}

	result := make([]storage.File, 0)
	marker := ""

	for {
		// 列出目录下的所有对象（无分隔符，递归获取）
		lsRes, err := fs.bucket.ListObjects(oss.Prefix(directory), oss.Marker(marker))
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range lsRes.Objects {
			// 跳过目录标记
			if obj.Key == directory || strings.HasSuffix(obj.Key, "/") {
				continue
			}

			visibility := "private"
			acl, err := fs.bucket.GetObjectACL(obj.Key)
			if err == nil && (acl.ACL == "public-read" || acl.ACL == "public-read-write") {
				visibility = "public"
			}

			file := &OSSFile{
				path:       obj.Key,
				name:       filepath.Base(obj.Key),
				size:       obj.Size,
				modTime:    obj.LastModified,
				isDir:      false,
				visibility: visibility,
				bucket:     fs.bucketName,
				publicURL:  fs.config.PublicURL,
			}

			result = append(result, file)
		}

		// 检查是否还有更多对象
		if lsRes.IsTruncated {
			marker = lsRes.NextMarker
		} else {
			break
		}
	}

	return result, nil
}

// Directories 实现storage.FileSystem接口
func (fs *OSSFileSystem) Directories(ctx context.Context, directory string) ([]string, error) {
	directory = normalizePath(directory)
	if directory != "" && !strings.HasSuffix(directory, "/") {
		directory += "/"
	}

	// 列出目录下的所有对象
	lsRes, err := fs.bucket.ListObjects(oss.Prefix(directory), oss.Delimiter("/"))
	if err != nil {
		return nil, fmt.Errorf("failed to list directories: %w", err)
	}

	result := make([]string, 0, len(lsRes.CommonPrefixes))
	for _, prefix := range lsRes.CommonPrefixes {
		// 移除前缀路径和结尾斜杠
		name := strings.TrimPrefix(prefix, directory)
		name = strings.TrimSuffix(name, "/")
		if name != "" {
			result = append(result, name)
		}
	}

	return result, nil
}

// AllDirectories 实现storage.FileSystem接口
func (fs *OSSFileSystem) AllDirectories(ctx context.Context, directory string) ([]string, error) {
	// OSS没有真正的目录层次结构，此方法需要手动构建
	// 这里简化实现，只返回一级目录
	return fs.Directories(ctx, directory)
}

// Copy 实现storage.FileSystem接口
func (fs *OSSFileSystem) Copy(ctx context.Context, source, destination string) error {
	source = normalizePath(source)
	destination = normalizePath(destination)

	// 检查源文件是否存在
	exists, err := fs.Exists(ctx, source)
	if err != nil {
		return err
	}
	if !exists {
		return storage.ErrFileNotFound
	}

	// 复制对象（同一个bucket内）
	_, err = fs.bucket.CopyObject(source, destination)
	if err != nil {
		return fmt.Errorf("failed to copy object: %w", err)
	}

	return nil
}

// Move 实现storage.FileSystem接口
func (fs *OSSFileSystem) Move(ctx context.Context, source, destination string) error {
	// 先复制，再删除
	if err := fs.Copy(ctx, source, destination); err != nil {
		return err
	}
	return fs.Delete(ctx, source)
}

// Size 实现storage.FileSystem接口
func (fs *OSSFileSystem) Size(ctx context.Context, path string) (int64, error) {
	path = normalizePath(path)

	// 获取对象元数据
	header, err := fs.bucket.GetObjectMeta(path)
	if err != nil {
		return 0, mapOSSError(err)
	}

	// 提取文件大小
	size := int64(0)
	if sizeStr := header.Get("Content-Length"); sizeStr != "" {
		fmt.Sscanf(sizeStr, "%d", &size)
	}

	return size, nil
}

// LastModified 实现storage.FileSystem接口
func (fs *OSSFileSystem) LastModified(ctx context.Context, path string) (time.Time, error) {
	path = normalizePath(path)

	// 获取对象元数据
	header, err := fs.bucket.GetObjectMeta(path)
	if err != nil {
		return time.Time{}, mapOSSError(err)
	}

	// 提取最后修改时间
	modTime := time.Time{}
	if lastModStr := header.Get("Last-Modified"); lastModStr != "" {
		modTime, _ = time.Parse(http.TimeFormat, lastModStr)
	}

	return modTime, nil
}

// MimeType 实现storage.FileSystem接口
func (fs *OSSFileSystem) MimeType(ctx context.Context, path string) (string, error) {
	path = normalizePath(path)

	// 获取对象元数据
	header, err := fs.bucket.GetObjectMeta(path)
	if err != nil {
		return "", mapOSSError(err)
	}

	// 提取内容类型
	contentType := header.Get("Content-Type")

	return contentType, nil
}

// SetVisibility 实现storage.FileSystem接口
func (fs *OSSFileSystem) SetVisibility(ctx context.Context, path, visibility string) error {
	path = normalizePath(path)

	// 检查文件是否存在
	exists, err := fs.Exists(ctx, path)
	if err != nil {
		return err
	}
	if !exists {
		return storage.ErrFileNotFound
	}

	// 设置OSS对象ACL
	var ossACL oss.ACLType
	switch visibility {
	case "public":
		ossACL = oss.ACLPublicRead
	case "private":
		ossACL = oss.ACLPrivate
	default:
		return fmt.Errorf("unsupported visibility: %s", visibility)
	}

	err = fs.bucket.SetObjectACL(path, ossACL)
	if err != nil {
		return mapOSSError(err)
	}

	return nil
}

// Visibility 实现storage.FileSystem接口
func (fs *OSSFileSystem) Visibility(ctx context.Context, path string) (string, error) {
	path = normalizePath(path)

	// 检查文件是否存在
	exists, err := fs.Exists(ctx, path)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", storage.ErrFileNotFound
	}

	// 获取对象ACL
	result, err := fs.bucket.GetObjectACL(path)
	if err != nil {
		return "", fmt.Errorf("failed to get visibility: %w", err)
	}

	// 根据ACL判断可见性
	visibility := "private"
	if result.ACL == "public-read" || result.ACL == "public-read-write" {
		visibility = "public"
	}

	return visibility, nil
}

// URL 实现storage.FileSystem接口
func (fs *OSSFileSystem) URL(ctx context.Context, path string) string {
	path = normalizePath(path)

	// 如果设置了公共URL，则使用它
	if fs.config.PublicURL != "" {
		baseURL := strings.TrimSuffix(fs.config.PublicURL, "/")
		return baseURL + "/" + path
	}

	// 否则根据配置构建URL
	scheme := "https"
	if !fs.config.UseSSL {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s.%s/%s", scheme, fs.bucketName, fs.config.Endpoint, path)
}

// TemporaryURL 实现storage.FileSystem接口
func (fs *OSSFileSystem) TemporaryURL(ctx context.Context, path string, expiration time.Duration) (string, error) {
	path = normalizePath(path)

	// 生成签名URL
	signURL, err := fs.bucket.SignURL(path, oss.HTTPGet, int64(expiration.Seconds()))
	if err != nil {
		return "", fmt.Errorf("failed to generate temporary url: %w", err)
	}

	return signURL, nil
}

// Checksum 实现storage.FileSystem接口
func (fs *OSSFileSystem) Checksum(ctx context.Context, path, algorithm string) (string, error) {
	path = normalizePath(path)

	// 获取对象元数据
	header, err := fs.bucket.GetObjectMeta(path)
	if err != nil {
		return "", mapOSSError(err)
	}

	// 获取ETag
	if algorithm == "md5" || algorithm == "etag" {
		etag := header.Get("ETag")
		etag = strings.Trim(etag, "\"")
		return etag, nil
	}

	return "", fmt.Errorf("unsupported checksum algorithm: %s", algorithm)
}

// 辅助函数

// 将OSS错误映射到storage错误
func mapOSSError(err error) error {
	if ossErr, ok := err.(oss.ServiceError); ok {
		if ossErr.StatusCode == 404 {
			return storage.ErrFileNotFound
		}
		if ossErr.StatusCode == 403 {
			return storage.ErrPermissionDenied
		}
	}
	return err
}

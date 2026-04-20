package cloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
	"github.com/zzliekkas/flow-storage/v3/core"
)

// COSConfig 腾讯云对象存储配置
type COSConfig struct {
	// 腾讯云 AppID
	AppID string

	// 腾讯云 SecretID
	SecretID string

	// 腾讯云 SecretKey
	SecretKey string

	// 存储桶名称
	Bucket string

	// 区域
	Region string

	// 是否使用HTTPS
	UseSSL bool

	// 自定义域名（CDN等）
	PublicURL string

	// 默认文件可见性（public 或 private）
	DefaultVisibility string

	// 临时URL过期时间（秒）
	UrlExpiry int64
}

// DefaultCOSConfig 返回默认COS配置
func DefaultCOSConfig() COSConfig {
	return COSConfig{
		UseSSL:            true,
		DefaultVisibility: "private",
		UrlExpiry:         3600, // 默认1小时
	}
}

// COSFileSystem 腾讯云对象存储文件系统
type COSFileSystem struct {
	// 客户端
	client *cos.Client

	// 配置
	config COSConfig
}

// NewCOS 创建新的腾讯云对象存储文件系统
func NewCOS(config COSConfig) (core.FileSystem, error) {
	if config.Bucket == "" {
		return nil, errors.New("cos: bucket不能为空")
	}

	if config.SecretID == "" || config.SecretKey == "" {
		return nil, errors.New("cos: SecretID和SecretKey不能为空")
	}

	if config.Region == "" {
		return nil, errors.New("cos: Region不能为空")
	}

	// 构建存储桶URL
	protocol := "https"
	if !config.UseSSL {
		protocol = "http"
	}

	bucketURL, err := url.Parse(fmt.Sprintf("%s://%s.cos.%s.myqcloud.com", protocol, config.Bucket, config.Region))
	if err != nil {
		return nil, fmt.Errorf("cos: 解析存储桶URL失败: %w", err)
	}

	// 初始化客户端
	b := &cos.BaseURL{BucketURL: bucketURL}
	client := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  config.SecretID,
			SecretKey: config.SecretKey,
		},
	})

	return &COSFileSystem{
		client: client,
		config: config,
	}, nil
}

// Get 获取指定路径的文件
func (fs *COSFileSystem) Get(ctx context.Context, path string) (core.File, error) {
	path = normalizePath(path)

	// 检查文件是否存在
	exists, err := fs.Exists(ctx, path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, core.ErrFileNotFound
	}

	// 获取文件属性
	resp, err := fs.client.Object.Head(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("cos: 获取文件属性失败: %w", err)
	}

	// 提取文件信息
	size := resp.ContentLength
	lastModified := time.Time{}
	if dateStr := resp.Header.Get("Last-Modified"); dateStr != "" {
		lastModified, _ = time.Parse(time.RFC1123, dateStr)
	}
	mimeType := resp.Header.Get("Content-Type")

	// 获取文件可见性
	acl := "private"
	aclResp, _, err := fs.client.Object.GetACL(ctx, path)
	if err == nil {
		// 检查是否有公共读权限
		for _, grant := range aclResp.AccessControlList {
			if grant.Grantee.URI == "http://cam.qcloud.com/groups/global/AllUsers" && grant.Permission == "READ" {
				acl = "public"
				break
			}
		}
	}

	// 创建文件对象
	file := &COSFile{
		fs:           fs,
		path:         path,
		name:         filepath.Base(path),
		size:         size,
		lastModified: lastModified,
		mimeType:     mimeType,
		visibility:   acl,
	}

	return file, nil
}

// Exists 检查文件是否存在
func (fs *COSFileSystem) Exists(ctx context.Context, path string) (bool, error) {
	path = normalizePath(path)

	_, err := fs.client.Object.Head(ctx, path, nil)
	if err != nil {
		// 检查是否是404错误
		if cos.IsNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("cos: 检查文件存在失败: %w", err)
	}

	return true, nil
}

// Write 写入文件内容
func (fs *COSFileSystem) Write(ctx context.Context, path string, content []byte, options ...core.WriteOption) error {
	path = normalizePath(path)

	// 应用写入选项
	opt := core.DefaultWriteOptions()
	for _, option := range options {
		option(opt)
	}

	// 检查文件是否存在
	if !opt.Overwrite {
		exists, err := fs.Exists(ctx, path)
		if err != nil {
			return err
		}
		if exists {
			return core.ErrFileAlreadyExists
		}
	}

	// 设置上传选项
	putOptions := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: opt.MimeType,
		},
	}

	// 上传文件
	_, err := fs.client.Object.Put(ctx, path, strings.NewReader(string(content)), putOptions)
	if err != nil {
		return fmt.Errorf("cos: 写入文件失败: %w", err)
	}

	// 设置可见性
	if opt.Visibility != "" {
		err = fs.SetVisibility(ctx, path, opt.Visibility)
		if err != nil {
			return fmt.Errorf("cos: 设置文件可见性失败: %w", err)
		}
	}

	return nil
}

// WriteStream 通过流写入文件
func (fs *COSFileSystem) WriteStream(ctx context.Context, path string, content io.Reader, options ...core.WriteOption) error {
	path = normalizePath(path)

	// 应用写入选项
	opt := core.DefaultWriteOptions()
	for _, option := range options {
		option(opt)
	}

	// 检查文件是否存在
	if !opt.Overwrite {
		exists, err := fs.Exists(ctx, path)
		if err != nil {
			return err
		}
		if exists {
			return core.ErrFileAlreadyExists
		}
	}

	// 设置上传选项
	putOptions := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: opt.MimeType,
		},
	}

	// 上传文件
	_, err := fs.client.Object.Put(ctx, path, content, putOptions)
	if err != nil {
		return fmt.Errorf("cos: 写入文件流失败: %w", err)
	}

	// 设置可见性
	if opt.Visibility != "" {
		err = fs.SetVisibility(ctx, path, opt.Visibility)
		if err != nil {
			return fmt.Errorf("cos: 设置文件可见性失败: %w", err)
		}
	}

	return nil
}

// Delete 删除文件
func (fs *COSFileSystem) Delete(ctx context.Context, path string) error {
	path = normalizePath(path)

	// 检查文件是否存在
	exists, err := fs.Exists(ctx, path)
	if err != nil {
		return err
	}
	if !exists {
		return core.ErrFileNotFound
	}

	// 删除文件
	_, err = fs.client.Object.Delete(ctx, path)
	if err != nil {
		return fmt.Errorf("cos: 删除文件失败: %w", err)
	}

	return nil
}

// DeleteDirectory 删除目录及其内容
func (fs *COSFileSystem) DeleteDirectory(ctx context.Context, path string) error {
	path = normalizeDirectory(path)

	// 列出目录下的所有对象
	opt := &cos.BucketGetOptions{
		Prefix:    path,
		MaxKeys:   1000,
		Delimiter: "",
	}

	var marker string
	isTruncated := true

	for isTruncated {
		opt.Marker = marker
		result, _, err := fs.client.Bucket.Get(ctx, opt)
		if err != nil {
			return fmt.Errorf("cos: 列出目录内容失败: %w", err)
		}

		// 没有内容，直接返回
		if len(result.Contents) == 0 {
			return nil
		}

		// 准备批量删除请求
		objects := make([]cos.Object, 0, len(result.Contents))
		for _, obj := range result.Contents {
			objects = append(objects, cos.Object{Key: obj.Key})
		}

		// 批量删除
		deleteOpt := &cos.ObjectDeleteMultiOptions{
			Objects: objects,
			Quiet:   true,
		}

		_, _, err = fs.client.Object.DeleteMulti(ctx, deleteOpt)
		if err != nil {
			return fmt.Errorf("cos: 批量删除目录内容失败: %w", err)
		}

		isTruncated = result.IsTruncated
		marker = result.NextMarker
	}

	return nil
}

// CreateDirectory 创建目录
func (fs *COSFileSystem) CreateDirectory(ctx context.Context, path string, options ...core.WriteOption) error {
	path = normalizeDirectory(path)

	// 在COS中，目录是通过以"/"结尾的空对象表示的
	// 应用写入选项
	opt := core.DefaultWriteOptions()
	for _, option := range options {
		option(opt)
	}

	// 创建目录对象
	putOptions := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: "application/x-directory",
		},
	}

	_, err := fs.client.Object.Put(ctx, path, strings.NewReader(""), putOptions)
	if err != nil {
		return fmt.Errorf("cos: 创建目录失败: %w", err)
	}

	// 设置可见性
	if opt.Visibility != "" {
		err = fs.SetVisibility(ctx, path, opt.Visibility)
		if err != nil {
			return fmt.Errorf("cos: 设置目录可见性失败: %w", err)
		}
	}

	return nil
}

// Files 列出目录下的所有文件
func (fs *COSFileSystem) Files(ctx context.Context, directory string) ([]core.File, error) {
	directory = normalizeDirectory(directory)

	// 设置列举选项
	opt := &cos.BucketGetOptions{
		Prefix:    directory,
		MaxKeys:   1000,
		Delimiter: "/",
	}

	result, _, err := fs.client.Bucket.Get(ctx, opt)
	if err != nil {
		return nil, fmt.Errorf("cos: 列出目录文件失败: %w", err)
	}

	files := make([]core.File, 0, len(result.Contents))
	for _, obj := range result.Contents {
		// 跳过目录自身和以/结尾的对象（目录）
		if obj.Key == directory || strings.HasSuffix(obj.Key, "/") {
			continue
		}

		file := &COSFile{
			fs:           fs,
			path:         obj.Key,
			name:         filepath.Base(obj.Key),
			size:         obj.Size,
			lastModified: parseTime(obj.LastModified),
			mimeType:     "", // 需要单独获取
		}

		files = append(files, file)
	}

	return files, nil
}

// AllFiles 递归列出目录下的所有文件
func (fs *COSFileSystem) AllFiles(ctx context.Context, directory string) ([]core.File, error) {
	directory = normalizeDirectory(directory)

	// 设置列举选项
	opt := &cos.BucketGetOptions{
		Prefix:  directory,
		MaxKeys: 1000,
	}

	var marker string
	isTruncated := true
	var files []core.File

	for isTruncated {
		opt.Marker = marker
		result, _, err := fs.client.Bucket.Get(ctx, opt)
		if err != nil {
			return nil, fmt.Errorf("cos: 递归列出目录文件失败: %w", err)
		}

		for _, obj := range result.Contents {
			// 跳过目录自身和以/结尾的对象（目录）
			if obj.Key == directory || strings.HasSuffix(obj.Key, "/") {
				continue
			}

			file := &COSFile{
				fs:           fs,
				path:         obj.Key,
				name:         filepath.Base(obj.Key),
				size:         obj.Size,
				lastModified: parseTime(obj.LastModified),
				mimeType:     "", // 需要单独获取
			}

			files = append(files, file)
		}

		isTruncated = result.IsTruncated
		marker = result.NextMarker
	}

	return files, nil
}

// Directories 列出目录下的所有子目录
func (fs *COSFileSystem) Directories(ctx context.Context, directory string) ([]string, error) {
	directory = normalizeDirectory(directory)

	// 设置列举选项
	opt := &cos.BucketGetOptions{
		Prefix:    directory,
		MaxKeys:   1000,
		Delimiter: "/",
	}

	result, _, err := fs.client.Bucket.Get(ctx, opt)
	if err != nil {
		return nil, fmt.Errorf("cos: 列出子目录失败: %w", err)
	}

	directories := make([]string, 0, len(result.CommonPrefixes))
	for _, prefix := range result.CommonPrefixes {
		// 去掉目录前缀和尾部斜杠
		relativeDir := strings.TrimPrefix(prefix, directory)
		relativeDir = strings.TrimSuffix(relativeDir, "/")
		if relativeDir != "" {
			directories = append(directories, relativeDir)
		}
	}

	return directories, nil
}

// AllDirectories 递归列出目录下的所有子目录
func (fs *COSFileSystem) AllDirectories(ctx context.Context, directory string) ([]string, error) {
	// 在COS中需要模拟实现，这里采用遍历目录的方式

	directory = normalizeDirectory(directory)

	// 设置列举选项
	opt := &cos.BucketGetOptions{
		Prefix:  directory,
		MaxKeys: 1000,
	}

	var marker string
	isTruncated := true
	dirMap := make(map[string]bool)

	for isTruncated {
		opt.Marker = marker
		result, _, err := fs.client.Bucket.Get(ctx, opt)
		if err != nil {
			return nil, fmt.Errorf("cos: 递归列出子目录失败: %w", err)
		}

		for _, obj := range result.Contents {
			if obj.Key == directory {
				continue
			}

			// 提取路径中的所有目录部分
			relPath := strings.TrimPrefix(obj.Key, directory)
			parts := strings.Split(relPath, "/")

			// 构建目录路径
			currentPath := ""
			for i := 0; i < len(parts)-1; i++ {
				if parts[i] == "" {
					continue
				}

				if currentPath == "" {
					currentPath = parts[i]
				} else {
					currentPath = currentPath + "/" + parts[i]
				}

				dirMap[currentPath] = true
			}
		}

		isTruncated = result.IsTruncated
		marker = result.NextMarker
	}

	// 将map转为slice
	directories := make([]string, 0, len(dirMap))
	for dir := range dirMap {
		directories = append(directories, dir)
	}

	return directories, nil
}

// Copy 复制文件
func (fs *COSFileSystem) Copy(ctx context.Context, source, destination string) error {
	source = normalizePath(source)
	destination = normalizePath(destination)

	// 检查源文件是否存在
	exists, err := fs.Exists(ctx, source)
	if err != nil {
		return err
	}
	if !exists {
		return core.ErrFileNotFound
	}

	// 检查目标文件是否存在
	exists, err = fs.Exists(ctx, destination)
	if err != nil {
		return err
	}
	if exists {
		return core.ErrFileAlreadyExists
	}

	// 构建复制源
	srcURL := fmt.Sprintf("%s/%s", fs.client.BaseURL.BucketURL.Host, source)

	// 复制对象
	_, _, err = fs.client.Object.Copy(ctx, destination, srcURL, nil)
	if err != nil {
		return fmt.Errorf("cos: 复制文件失败: %w", err)
	}

	return nil
}

// Move 移动文件
func (fs *COSFileSystem) Move(ctx context.Context, source, destination string) error {
	// 先复制，后删除
	err := fs.Copy(ctx, source, destination)
	if err != nil {
		return err
	}

	return fs.Delete(ctx, source)
}

// Size 获取文件大小
func (fs *COSFileSystem) Size(ctx context.Context, path string) (int64, error) {
	path = normalizePath(path)

	// 获取文件属性
	resp, err := fs.client.Object.Head(ctx, path, nil)
	if err != nil {
		if cos.IsNotFoundError(err) {
			return 0, core.ErrFileNotFound
		}
		return 0, fmt.Errorf("cos: 获取文件大小失败: %w", err)
	}

	return resp.ContentLength, nil
}

// LastModified 获取文件修改时间
func (fs *COSFileSystem) LastModified(ctx context.Context, path string) (time.Time, error) {
	path = normalizePathCOS(path)

	resp, err := fs.client.Object.Head(ctx, path, nil)
	if err != nil {
		if cos.IsNotFoundError(err) {
			return time.Time{}, core.ErrFileNotFound
		}
		return time.Time{}, fmt.Errorf("cos: 获取文件修改时间失败: %w", err)
	}

	// 从 header 中获取修改时间
	lastModified := time.Time{}
	if dateStr := resp.Header.Get("Last-Modified"); dateStr != "" {
		lastModified, _ = time.Parse(time.RFC1123, dateStr)
	}

	return lastModified, nil
}

// MimeType 获取文件MIME类型
func (fs *COSFileSystem) MimeType(ctx context.Context, path string) (string, error) {
	path = normalizePath(path)

	// 获取文件属性
	resp, err := fs.client.Object.Head(ctx, path, nil)
	if err != nil {
		if cos.IsNotFoundError(err) {
			return "", core.ErrFileNotFound
		}
		return "", fmt.Errorf("cos: 获取文件MIME类型失败: %w", err)
	}

	return resp.Header.Get("Content-Type"), nil
}

// SetVisibility 设置文件可见性
func (fs *COSFileSystem) SetVisibility(ctx context.Context, path string, visibility string) error {
	path = normalizePath(path)

	// 转换可见性为COS ACL
	var acl string
	switch visibility {
	case "public":
		acl = "public-read"
	case "private":
		acl = "private"
	default:
		return fmt.Errorf("cos: 不支持的可见性: %s", visibility)
	}

	// 设置ACL
	opt := &cos.ObjectPutACLOptions{
		Header: &cos.ACLHeaderOptions{
			XCosACL: acl,
		},
	}

	_, err := fs.client.Object.PutACL(ctx, path, opt)
	if err != nil {
		return fmt.Errorf("cos: 设置文件可见性失败: %w", err)
	}

	return nil
}

// Visibility 获取文件可见性
func (fs *COSFileSystem) Visibility(ctx context.Context, path string) (string, error) {
	path = normalizePathCOS(path)

	// 获取ACL
	resp, _, err := fs.client.Object.GetACL(ctx, path)
	if err != nil {
		if cos.IsNotFoundError(err) {
			return "", core.ErrFileNotFound
		}
		return "", fmt.Errorf("cos: 获取文件可见性失败: %w", err)
	}

	// 默认为私有
	visibility := "private"

	// 检查是否有公共读权限
	for _, grant := range resp.AccessControlList {
		if grant.Grantee.URI == "http://cam.qcloud.com/groups/global/AllUsers" && grant.Permission == "READ" {
			visibility = "public"
			break
		}
	}

	return visibility, nil
}

// URL 获取文件URL
func (fs *COSFileSystem) URL(ctx context.Context, path string) string {
	path = normalizePath(path)

	// 如果设置了自定义域名，使用自定义域名
	if fs.config.PublicURL != "" {
		return fmt.Sprintf("%s/%s", strings.TrimRight(fs.config.PublicURL, "/"), path)
	}

	// 对于公共文件，返回对象URL
	visibility, err := fs.Visibility(ctx, path)
	if err == nil && visibility == "public" {
		return fmt.Sprintf("%s/%s", fs.client.BaseURL.BucketURL.String(), path)
	}

	// 默认返回临时URL
	url, _ := fs.TemporaryURL(ctx, path, time.Duration(fs.config.UrlExpiry)*time.Second)
	return url
}

// TemporaryURL 获取临时URL
func (fs *COSFileSystem) TemporaryURL(ctx context.Context, path string, expiration time.Duration) (string, error) {
	path = normalizePath(path)

	// 检查文件是否存在
	exists, err := fs.Exists(ctx, path)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", core.ErrFileNotFound
	}

	// 生成预签名URL
	opt := &cos.PresignedURLOptions{
		Query:  &url.Values{},
		Header: &http.Header{},
	}

	presignedURL, err := fs.client.Object.GetPresignedURL(ctx, http.MethodGet, path, fs.config.SecretID, fs.config.SecretKey, expiration, opt)
	if err != nil {
		return "", fmt.Errorf("cos: 生成临时URL失败: %w", err)
	}

	return presignedURL.String(), nil
}

// Checksum 计算文件校验和
func (fs *COSFileSystem) Checksum(ctx context.Context, path string, algorithm string) (string, error) {
	path = normalizePath(path)

	// 目前仅支持COS内置的ETag
	if algorithm != "etag" && algorithm != "md5" {
		return "", fmt.Errorf("cos: 不支持的校验和算法: %s", algorithm)
	}

	// 获取对象属性
	resp, err := fs.client.Object.Head(ctx, path, nil)
	if err != nil {
		if cos.IsNotFoundError(err) {
			return "", core.ErrFileNotFound
		}
		return "", fmt.Errorf("cos: 获取文件校验和失败: %w", err)
	}

	// 对于COS，ETag通常是MD5值（除非是分块上传），但它带有双引号
	etag := resp.Header.Get("ETag")
	etag = strings.Trim(etag, "\"")

	return etag, nil
}

// COSFile 表示COS中的文件
type COSFile struct {
	// 所属文件系统
	fs *COSFileSystem

	// 文件路径
	path string

	// 文件名
	name string

	// 文件大小
	size int64

	// 最后修改时间
	lastModified time.Time

	// MIME类型
	mimeType string

	// 文件可见性
	visibility string

	// 是否为目录
	isDirectory bool
}

// Path 返回文件的路径
func (f *COSFile) Path() string {
	return f.path
}

// Name 返回文件的名称
func (f *COSFile) Name() string {
	return f.name
}

// Extension 返回文件的扩展名
func (f *COSFile) Extension() string {
	return strings.TrimPrefix(filepath.Ext(f.name), ".")
}

// Size 返回文件的大小
func (f *COSFile) Size() int64 {
	return f.size
}

// LastModified 返回文件的最后修改时间
func (f *COSFile) LastModified() time.Time {
	return f.lastModified
}

// IsDirectory 判断是否为目录
func (f *COSFile) IsDirectory() bool {
	return f.isDirectory
}

// Read 读取文件内容
func (f *COSFile) Read(ctx context.Context) ([]byte, error) {
	resp, err := f.fs.client.Object.Get(ctx, f.path, nil)
	if err != nil {
		return nil, fmt.Errorf("cos: 读取文件内容失败: %w", err)
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cos: 读取文件内容失败: %w", err)
	}

	return content, nil
}

// ReadStream 获取文件的读取流
func (f *COSFile) ReadStream(ctx context.Context) (io.ReadCloser, error) {
	resp, err := f.fs.client.Object.Get(ctx, f.path, nil)
	if err != nil {
		return nil, fmt.Errorf("cos: 获取文件读取流失败: %w", err)
	}

	return resp.Body, nil
}

// MimeType 返回文件的MIME类型
func (f *COSFile) MimeType() string {
	// 如果还没获取MIME类型，立即获取
	if f.mimeType == "" {
		ctx := context.Background()
		mimeType, err := f.fs.MimeType(ctx, f.path)
		if err == nil {
			f.mimeType = mimeType
		}
	}

	return f.mimeType
}

// Visibility 返回文件的可见性
func (f *COSFile) Visibility() string {
	// 如果还没获取可见性，立即获取
	if f.visibility == "" {
		ctx := context.Background()
		visibility, err := f.fs.Visibility(ctx, f.path)
		if err == nil {
			f.visibility = visibility
		}
	}

	return f.visibility
}

// URL 获取文件的URL
func (f *COSFile) URL() string {
	ctx := context.Background()
	return f.fs.URL(ctx, f.path)
}

// TemporaryURL 获取文件的临时URL
func (f *COSFile) TemporaryURL(ctx context.Context, expiration time.Duration) (string, error) {
	return f.fs.TemporaryURL(ctx, f.path, expiration)
}

// Metadata 获取文件的元数据
func (f *COSFile) Metadata() map[string]interface{} {
	metadata := make(map[string]interface{})
	metadata["path"] = f.path
	metadata["name"] = f.name
	metadata["size"] = f.size
	metadata["mimeType"] = f.MimeType()
	metadata["lastModified"] = f.lastModified
	metadata["visibility"] = f.Visibility()
	metadata["isDirectory"] = f.isDirectory

	return metadata
}

// 辅助函数

// normalizePathCOS 标准化路径
func normalizePathCOS(path string) string {
	// 移除前导斜杠
	path = strings.TrimPrefix(path, "/")
	return path
}

// normalizeDirectory 标准化目录路径
func normalizeDirectory(path string) string {
	// 确保目录路径以斜杠结尾
	path = normalizePathCOS(path)
	if path != "" && !strings.HasSuffix(path, "/") {
		path = path + "/"
	}
	return path
}

// 辅助函数，将时间字符串解析为 time.Time
func parseTime(timeStr string) time.Time {
	t, err := time.Parse(time.RFC3339, timeStr)
	if err == nil {
		return t
	}

	// 尝试其他时间格式
	layouts := []string{
		time.RFC1123,
		time.RFC1123Z,
		"2006-01-02T15:04:05Z",
		"Mon, 02 Jan 2006 15:04:05 MST",
	}

	for _, layout := range layouts {
		t, err := time.Parse(layout, timeStr)
		if err == nil {
			return t
		}
	}

	return time.Time{}
}

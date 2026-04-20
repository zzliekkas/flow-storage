package cloud

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/zzliekkas/flow-storage/v3"
	"github.com/zzliekkas/flow-storage/v3/core"
)

// S3Config S3配置选项
type S3Config struct {
	// Endpoint 端点URL
	Endpoint string

	// Region 区域
	Region string

	// Bucket 存储桶名称
	Bucket string

	// AccessKey 访问密钥ID
	AccessKey string

	// SecretKey 访问密钥
	SecretKey string

	// UseSSL 是否使用SSL
	UseSSL bool

	// PublicURL 公共URL前缀
	PublicURL string

	// ForcePathStyle 是否强制使用路径风格的URL
	ForcePathStyle bool

	// DefaultVisibility 默认可见性
	DefaultVisibility string

	// 是否禁用虚拟主机样式寻址
	DisableVirtualHostStyle bool
}

// DefaultS3Config 返回默认S3配置
func DefaultS3Config() S3Config {
	return S3Config{
		Region:            "us-east-1",
		UseSSL:            true,
		ForcePathStyle:    false,
		DefaultVisibility: "private",
	}
}

// S3File 表示S3上的文件
type S3File struct {
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
func (f *S3File) Path() string {
	return f.path
}

// Name 实现storage.File接口
func (f *S3File) Name() string {
	return f.name
}

// Extension 实现storage.File接口
func (f *S3File) Extension() string {
	return filepath.Ext(f.name)
}

// Size 实现storage.File接口
func (f *S3File) Size() int64 {
	return f.size
}

// LastModified 实现storage.File接口
func (f *S3File) LastModified() time.Time {
	return f.modTime
}

// IsDirectory 实现storage.File接口
func (f *S3File) IsDirectory() bool {
	return f.isDir
}

// Read 实现storage.File接口
func (f *S3File) Read(ctx context.Context) ([]byte, error) {
	if f.isDir {
		return nil, fmt.Errorf("cannot read directory: %s", f.path)
	}

	// 由于S3File不持有client引用，这个方法不完整
	// 实际实现应在S3FileSystem中，并传递reader给此方法
	return nil, errors.New("s3 file read not implemented directly, use filesystem instead")
}

// ReadStream 实现storage.File接口
func (f *S3File) ReadStream(ctx context.Context) (io.ReadCloser, error) {
	if f.isDir {
		return nil, fmt.Errorf("cannot read directory: %s", f.path)
	}

	// 同上
	return nil, errors.New("s3 file read stream not implemented directly, use filesystem instead")
}

// MimeType 实现storage.File接口
func (f *S3File) MimeType() string {
	return f.contentType
}

// Visibility 实现storage.File接口
func (f *S3File) Visibility() string {
	return f.visibility
}

// URL 实现storage.File接口
func (f *S3File) URL() string {
	if f.publicURL != "" && f.visibility == "public" {
		if strings.HasSuffix(f.publicURL, "/") {
			return f.publicURL + f.path
		}
		return f.publicURL + "/" + f.path
	}
	return ""
}

// TemporaryURL 实现storage.File接口
func (f *S3File) TemporaryURL(ctx context.Context, expiration time.Duration) (string, error) {
	// 同上，需要在S3FileSystem中实现
	return "", errors.New("s3 temporary url not implemented directly, use filesystem instead")
}

// Metadata 实现storage.File接口
func (f *S3File) Metadata() map[string]interface{} {
	return f.metadata
}

// S3FileSystem 实现基于S3的文件系统
type S3FileSystem struct {
	client *s3.Client
	config S3Config
}

// New 创建新的S3文件系统
func New(cfg S3Config) (*S3FileSystem, error) {
	// 创建自定义凭证提供者
	creds := credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")

	// 创建AWS配置
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if service == s3.ServiceID && cfg.Endpoint != "" {
			return aws.Endpoint{
				URL:               cfg.Endpoint,
				HostnameImmutable: true,
				SigningRegion:     cfg.Region,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(creds),
		config.WithEndpointResolverWithOptions(customResolver),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load s3 config: %w", err)
	}

	// 创建S3客户端
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.ForcePathStyle
	})

	return &S3FileSystem{
		client: client,
		config: cfg,
	}, nil
}

// Get 实现storage.FileSystem接口
func (fs *S3FileSystem) Get(ctx context.Context, path string) (core.File, error) {
	path = normalizePath(path)

	// 如果路径以/结尾，认为是目录
	if strings.HasSuffix(path, "/") {
		return &S3File{
			path:       path,
			name:       filepath.Base(strings.TrimSuffix(path, "/")),
			isDir:      true,
			bucket:     fs.config.Bucket,
			visibility: fs.config.DefaultVisibility,
			publicURL:  fs.config.PublicURL,
		}, nil
	}

	// 获取对象
	headObj, err := fs.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.config.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return nil, storage.ErrFileNotFound
		}
		return nil, fmt.Errorf("failed to get object head: %w", err)
	}

	// 提取MIME类型
	contentType := ""
	if headObj.ContentType != nil {
		contentType = *headObj.ContentType
	}

	// 提取可见性
	visibility := fs.config.DefaultVisibility

	// 提取元数据
	metadata := make(map[string]interface{})
	for k, v := range headObj.Metadata {
		metadata[k] = v
	}

	file := &S3File{
		path:        path,
		name:        filepath.Base(path),
		size:        *headObj.ContentLength,
		modTime:     *headObj.LastModified,
		isDir:       false,
		contentType: contentType,
		visibility:  visibility,
		bucket:      fs.config.Bucket,
		publicURL:   fs.config.PublicURL,
		metadata:    metadata,
	}

	return file, nil
}

// Exists 实现storage.FileSystem接口
func (fs *S3FileSystem) Exists(ctx context.Context, path string) (bool, error) {
	path = normalizePath(path)

	_, err := fs.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.config.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check object existence: %w", err)
	}
	return true, nil
}

// Write 实现storage.FileSystem接口
func (fs *S3FileSystem) Write(ctx context.Context, path string, content []byte, options ...core.WriteOption) error {
	path = normalizePath(path)

	// 应用写入选项
	opts := core.DefaultWriteOptions()
	for _, option := range options {
		option(opts)
	}

	// 如果不允许覆盖，检查文件是否已存在
	if !opts.Overwrite {
		exists, err := fs.Exists(ctx, path)
		if err != nil {
			return err
		}
		if exists {
			return storage.ErrFileAlreadyExists
		}
	}

	// 确定Content-Type
	contentType := opts.MimeType
	if contentType == "" {
		contentType = detectContentType(content, path)
	}

	// 确定ACL
	var acl types.ObjectCannedACL
	if opts.Visibility == "public" {
		acl = types.ObjectCannedACLPublicRead
	} else {
		acl = types.ObjectCannedACLPrivate
	}

	// 将自定义元数据转换为S3元数据
	metadata := make(map[string]string)
	for k, v := range opts.Metadata {
		if str, ok := v.(string); ok {
			metadata[k] = str
		} else {
			// 尝试转换为字符串
			metadata[k] = fmt.Sprintf("%v", v)
		}
	}

	// 上传内容
	_, err := fs.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(fs.config.Bucket),
		Key:         aws.String(path),
		Body:        bytes.NewReader(content),
		ContentType: aws.String(contentType),
		ACL:         acl,
		Metadata:    metadata,
	})
	if err != nil {
		return fmt.Errorf("failed to upload object: %w", err)
	}

	return nil
}

// WriteStream 实现storage.FileSystem接口
func (fs *S3FileSystem) WriteStream(ctx context.Context, path string, content io.Reader, options ...core.WriteOption) error {
	// 读取整个流
	data, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("failed to read content stream: %w", err)
	}

	// 调用Write方法
	return fs.Write(ctx, path, data, options...)
}

// Delete 实现storage.FileSystem接口
func (fs *S3FileSystem) Delete(ctx context.Context, path string) error {
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
	_, err = fs.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(fs.config.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	return nil
}

// DeleteDirectory 实现storage.FileSystem接口
func (fs *S3FileSystem) DeleteDirectory(ctx context.Context, path string) error {
	path = normalizePath(path)
	if path != "" && !strings.HasSuffix(path, "/") {
		path += "/"
	}

	// 列出目录下的对象
	listParams := &s3.ListObjectsV2Input{
		Bucket: aws.String(fs.config.Bucket),
		Prefix: aws.String(path),
	}

	paginator := s3.NewListObjectsV2Paginator(fs.client, listParams)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		objectsToDelete := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			objectsToDelete = append(objectsToDelete, types.ObjectIdentifier{
				Key: obj.Key,
			})
		}

		if len(objectsToDelete) > 0 {
			_, err = fs.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(fs.config.Bucket),
				Delete: &types.Delete{
					Objects: objectsToDelete,
					Quiet:   aws.Bool(true),
				},
			})
			if err != nil {
				return fmt.Errorf("failed to delete objects: %w", err)
			}
		}
	}

	return nil
}

// CreateDirectory 实现storage.FileSystem接口
func (fs *S3FileSystem) CreateDirectory(ctx context.Context, path string, options ...core.WriteOption) error {
	path = normalizePath(path)
	if path == "" {
		return nil // 根目录已存在
	}

	if !strings.HasSuffix(path, "/") {
		path += "/"
	}

	// 应用写入选项
	opts := core.DefaultWriteOptions()
	for _, option := range options {
		option(opts)
	}

	// 确定ACL
	var acl types.ObjectCannedACL
	if opts.Visibility == "public" {
		acl = types.ObjectCannedACLPublicRead
	} else {
		acl = types.ObjectCannedACLPrivate
	}

	// 创建一个空对象作为目录标记
	_, err := fs.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(fs.config.Bucket),
		Key:         aws.String(path),
		Body:        bytes.NewReader([]byte{}),
		ContentType: aws.String("application/x-directory"),
		ACL:         acl,
	})
	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return nil
}

// Files 实现storage.FileSystem接口
func (fs *S3FileSystem) Files(ctx context.Context, directory string) ([]core.File, error) {
	directory = normalizePath(directory)
	if directory != "" && !strings.HasSuffix(directory, "/") {
		directory += "/"
	}

	// 列出目录下的对象
	listParams := &s3.ListObjectsV2Input{
		Bucket:    aws.String(fs.config.Bucket),
		Prefix:    aws.String(directory),
		Delimiter: aws.String("/"),
	}

	result := make([]core.File, 0)
	paginator := s3.NewListObjectsV2Paginator(fs.client, listParams)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		// 处理文件
		for _, obj := range page.Contents {
			// 跳过目录标记
			key := *obj.Key
			if key == directory || strings.HasSuffix(key, "/") {
				continue
			}

			// 创建文件对象
			file := &S3File{
				path:      key,
				name:      filepath.Base(key),
				size:      *obj.Size,
				modTime:   *obj.LastModified,
				isDir:     false,
				bucket:    fs.config.Bucket,
				publicURL: fs.config.PublicURL,
			}

			result = append(result, file)
		}
	}

	return result, nil
}

// AllFiles 实现storage.FileSystem接口
func (fs *S3FileSystem) AllFiles(ctx context.Context, directory string) ([]core.File, error) {
	directory = normalizePath(directory)
	if directory != "" && !strings.HasSuffix(directory, "/") {
		directory += "/"
	}

	// 列出目录下的所有对象（无分隔符，递归获取）
	listParams := &s3.ListObjectsV2Input{
		Bucket: aws.String(fs.config.Bucket),
		Prefix: aws.String(directory),
	}

	result := make([]core.File, 0)
	paginator := s3.NewListObjectsV2Paginator(fs.client, listParams)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			key := *obj.Key
			// 跳过目录标记
			if key == directory || strings.HasSuffix(key, "/") {
				continue
			}

			file := &S3File{
				path:      key,
				name:      filepath.Base(key),
				size:      *obj.Size,
				modTime:   *obj.LastModified,
				isDir:     false,
				bucket:    fs.config.Bucket,
				publicURL: fs.config.PublicURL,
			}

			result = append(result, file)
		}
	}

	return result, nil
}

// Directories 实现storage.FileSystem接口
func (fs *S3FileSystem) Directories(ctx context.Context, directory string) ([]string, error) {
	directory = normalizePath(directory)
	if directory != "" && !strings.HasSuffix(directory, "/") {
		directory += "/"
	}

	// 列出目录
	listParams := &s3.ListObjectsV2Input{
		Bucket:    aws.String(fs.config.Bucket),
		Prefix:    aws.String(directory),
		Delimiter: aws.String("/"),
	}

	result := make([]string, 0)
	paginator := s3.NewListObjectsV2Paginator(fs.client, listParams)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list directories: %w", err)
		}

		for _, prefix := range page.CommonPrefixes {
			// 移除前缀路径和结尾斜杠
			name := strings.TrimPrefix(*prefix.Prefix, directory)
			name = strings.TrimSuffix(name, "/")
			if name != "" {
				result = append(result, name)
			}
		}
	}

	return result, nil
}

// AllDirectories 实现storage.FileSystem接口
func (fs *S3FileSystem) AllDirectories(ctx context.Context, directory string) ([]string, error) {
	// S3没有真正的目录层次结构，此方法需要手动构建
	// 这里简化实现，只返回一级目录
	return fs.Directories(ctx, directory)
}

// Copy 实现storage.FileSystem接口
func (fs *S3FileSystem) Copy(ctx context.Context, source, destination string) error {
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

	// 构建复制源
	copySource := fmt.Sprintf("%s/%s", fs.config.Bucket, source)

	// 复制对象
	_, err = fs.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(fs.config.Bucket),
		CopySource: aws.String(copySource),
		Key:        aws.String(destination),
	})
	if err != nil {
		return fmt.Errorf("failed to copy object: %w", err)
	}

	return nil
}

// Move 实现storage.FileSystem接口
func (fs *S3FileSystem) Move(ctx context.Context, source, destination string) error {
	// 先复制，再删除
	if err := fs.Copy(ctx, source, destination); err != nil {
		return err
	}
	return fs.Delete(ctx, source)
}

// Size 实现storage.FileSystem接口
func (fs *S3FileSystem) Size(ctx context.Context, path string) (int64, error) {
	path = normalizePath(path)

	headObj, err := fs.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.config.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return 0, storage.ErrFileNotFound
		}
		return 0, fmt.Errorf("failed to get object head: %w", err)
	}

	return *headObj.ContentLength, nil
}

// LastModified 实现storage.FileSystem接口
func (fs *S3FileSystem) LastModified(ctx context.Context, path string) (time.Time, error) {
	path = normalizePath(path)

	headObj, err := fs.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.config.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return time.Time{}, storage.ErrFileNotFound
		}
		return time.Time{}, fmt.Errorf("failed to get object head: %w", err)
	}

	return *headObj.LastModified, nil
}

// MimeType 实现storage.FileSystem接口
func (fs *S3FileSystem) MimeType(ctx context.Context, path string) (string, error) {
	path = normalizePath(path)

	headObj, err := fs.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.config.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return "", storage.ErrFileNotFound
		}
		return "", fmt.Errorf("failed to get object head: %w", err)
	}

	if headObj.ContentType == nil {
		return "", nil
	}
	return *headObj.ContentType, nil
}

// SetVisibility 实现storage.FileSystem接口
func (fs *S3FileSystem) SetVisibility(ctx context.Context, path, visibility string) error {
	path = normalizePath(path)

	var acl types.ObjectCannedACL
	if visibility == "public" {
		acl = types.ObjectCannedACLPublicRead
	} else {
		acl = types.ObjectCannedACLPrivate
	}

	_, err := fs.client.PutObjectAcl(ctx, &s3.PutObjectAclInput{
		Bucket: aws.String(fs.config.Bucket),
		Key:    aws.String(path),
		ACL:    acl,
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return storage.ErrFileNotFound
		}
		return fmt.Errorf("failed to set visibility: %w", err)
	}

	return nil
}

// Visibility 实现storage.FileSystem接口
func (fs *S3FileSystem) Visibility(ctx context.Context, path string) (string, error) {
	path = normalizePath(path)

	// 获取对象ACL
	result, err := fs.client.GetObjectAcl(ctx, &s3.GetObjectAclInput{
		Bucket: aws.String(fs.config.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return "", storage.ErrFileNotFound
		}
		return "", fmt.Errorf("failed to get visibility: %w", err)
	}

	// 检查是否公开可读
	visibility := "private"
	for _, grant := range result.Grants {
		if grant.Grantee != nil && grant.Grantee.Type == types.TypeGroup {
			if grant.Grantee.URI != nil && *grant.Grantee.URI == "http://acs.amazonaws.com/groups/global/AllUsers" {
				if grant.Permission == types.PermissionRead {
					visibility = "public"
					break
				}
			}
		}
	}

	return visibility, nil
}

// URL 实现storage.FileSystem接口
func (fs *S3FileSystem) URL(ctx context.Context, path string) string {
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

	if fs.config.ForcePathStyle {
		return fmt.Sprintf("%s://%s/%s/%s", scheme, fs.config.Endpoint, fs.config.Bucket, path)
	}

	return fmt.Sprintf("%s://%s.%s/%s", scheme, fs.config.Bucket, fs.config.Endpoint, path)
}

// TemporaryURL 实现storage.FileSystem接口
func (fs *S3FileSystem) TemporaryURL(ctx context.Context, path string, expiration time.Duration) (string, error) {
	path = normalizePath(path)

	// 创建 presign 客户端
	presignClient := s3.NewPresignClient(fs.client)

	// 创建预签名URL
	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(fs.config.Bucket),
		Key:    aws.String(path),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiration
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate temporary url: %w", err)
	}

	return request.URL, nil
}

// Checksum 实现storage.FileSystem接口
func (fs *S3FileSystem) Checksum(ctx context.Context, path, algorithm string) (string, error) {
	path = normalizePath(path)

	// S3 只支持 ETag (通常是 MD5)
	if algorithm != "md5" && algorithm != "etag" {
		return "", fmt.Errorf("unsupported checksum algorithm: %s", algorithm)
	}

	headObj, err := fs.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(fs.config.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return "", storage.ErrFileNotFound
		}
		return "", fmt.Errorf("failed to get object head: %w", err)
	}

	if headObj.ETag == nil {
		return "", fmt.Errorf("etag not available")
	}

	// 移除ETag中的引号
	etag := *headObj.ETag
	etag = strings.Trim(etag, "\"")
	return etag, nil
}

// 辅助函数

// 规范化路径
func normalizePath(path string) string {
	// 移除开头的斜杠
	path = strings.TrimPrefix(path, "/")
	// 使用正斜杠
	path = strings.ReplaceAll(path, "\\", "/")
	return path
}

// 检测内容类型
func detectContentType(content []byte, filename string) string {
	// 首先尝试从内容检测
	contentType := http.DetectContentType(content)

	// 如果无法确定或者是通用二进制类型，则尝试从文件扩展名判断
	if contentType == "application/octet-stream" {
		ext := strings.ToLower(filepath.Ext(filename))
		switch ext {
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
		case ".pdf":
			return "application/pdf"
		case ".png":
			return "image/png"
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".gif":
			return "image/gif"
		case ".svg":
			return "image/svg+xml"
		case ".mp3":
			return "audio/mpeg"
		case ".mp4":
			return "video/mp4"
		case ".webm":
			return "video/webm"
		case ".zip":
			return "application/zip"
		case ".doc", ".docx":
			return "application/msword"
		case ".xls", ".xlsx":
			return "application/vnd.ms-excel"
		case ".ppt", ".pptx":
			return "application/vnd.ms-powerpoint"
		}
	}

	return contentType
}

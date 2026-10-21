package cloud

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	qiniuauth "github.com/qiniu/go-sdk/v7/auth/qbox"
	qiniuclient "github.com/qiniu/go-sdk/v7/client"
	qiniustorage "github.com/qiniu/go-sdk/v7/storage"
	"github.com/zzliekkas/flow-storage/v3"
)

// QiniuConfig 七牛云配置
type QiniuConfig struct {
	AccessKey string
	SecretKey string
	Bucket    string
	Domain    string // 空间绑定的访问域名（含协议），如 https://cdn.example.com
}

// QiniuFileSystem 七牛云文件系统。
//
// 七牛云只有空间（Bucket）级 ACL，没有对象级 public/private，因此：
//   - URL 返回空间域名拼接的公开地址；
//   - TemporaryURL 用七牛私有下载凭证生成带有效期的签名地址（公开空间同样可用）；
//   - SetVisibility 返回 ErrVisibilityNotSupported，公开资源请使用独立的公开空间。
type QiniuFileSystem struct {
	config  QiniuConfig
	mac     *qiniuauth.Mac
	manager *qiniustorage.BucketManager

	privacyMu sync.Mutex
	private   *bool
}

var _ storage.FileSystem = (*QiniuFileSystem)(nil)

// NewQiniu 创建七牛云文件系统
func NewQiniu(cfg QiniuConfig) (*QiniuFileSystem, error) {
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, errors.New("qiniu: accessKey 与 secretKey 不能为空")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("qiniu: bucket 不能为空")
	}

	mac := qiniuauth.NewMac(cfg.AccessKey, cfg.SecretKey)
	return &QiniuFileSystem{
		config:  cfg,
		mac:     mac,
		manager: qiniustorage.NewBucketManager(mac, &qiniustorage.Config{}),
	}, nil
}

// domain 返回去掉尾部斜杠的访问域名；未带协议时默认补 https://
func (fs *QiniuFileSystem) domain() (string, error) {
	domain := strings.TrimRight(strings.TrimSpace(fs.config.Domain), "/")
	if domain == "" {
		return "", errors.New("qiniu: 未配置 Domain，无法生成访问链接")
	}
	if !strings.Contains(domain, "://") {
		domain = "https://" + domain
	}
	return domain, nil
}

// bucketPrivate 查询并缓存空间是否为私有空间
func (fs *QiniuFileSystem) bucketPrivate() (bool, error) {
	fs.privacyMu.Lock()
	defer fs.privacyMu.Unlock()

	if fs.private != nil {
		return *fs.private, nil
	}
	info, err := fs.manager.GetBucketInfo(fs.config.Bucket)
	if err != nil {
		return false, fmt.Errorf("qiniu: 查询空间信息失败: %w", err)
	}
	private := info.Private == 1
	fs.private = &private
	return private, nil
}

// isQiniuNotFound 判断错误是否为「对象不存在」（HTTP 612 / no such file）
func isQiniuNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, qiniustorage.ErrNoSuchFile) {
		return true
	}
	var info *qiniuclient.ErrorInfo
	if errors.As(err, &info) {
		return info.Code == 612 || info.Errno == 612
	}
	return strings.Contains(strings.ToLower(err.Error()), "no such file")
}

// qiniuPutTime 把七牛 PutTime（单位 100 纳秒）转成 time.Time
func qiniuPutTime(putTime int64) time.Time {
	if putTime <= 0 {
		return time.Time{}
	}
	return time.Unix(0, putTime*100)
}

// Upload 上传文件到七牛云（表单上传，适合已知大小的小文件）
func (fs *QiniuFileSystem) Upload(ctx context.Context, key string, reader io.Reader, size int64, mimeType string) error {
	key = qiniuKey(key)
	if key == "" {
		return storage.ErrInvalidPath
	}

	putPolicy := qiniustorage.PutPolicy{Scope: fs.config.Bucket}
	uptoken := putPolicy.UploadToken(fs.mac)
	uploader := qiniustorage.NewFormUploader(&qiniustorage.Config{})
	ret := qiniustorage.PutRet{}
	extra := qiniustorage.PutExtra{MimeType: mimeType}
	if err := uploader.Put(ctx, &ret, uptoken, key, reader, size, &extra); err != nil {
		return fmt.Errorf("qiniu: 上传失败: %w", err)
	}
	return nil
}

// Download 获取文件下载流
func (fs *QiniuFileSystem) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return fs.openStream(ctx, qiniuKey(key))
}

// GetURL 获取文件公开 URL
func (fs *QiniuFileSystem) GetURL(ctx context.Context, key string, _ time.Duration) (string, error) {
	url := fs.URL(ctx, qiniuKey(key))
	if url == "" {
		return "", errors.New("qiniu: 未配置 Domain，无法生成访问链接")
	}
	return url, nil
}

// openStream 通过签名地址打开文件流
func (fs *QiniuFileSystem) openStream(ctx context.Context, key string) (io.ReadCloser, error) {
	url, err := fs.TemporaryURL(ctx, key, time.Hour)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("qiniu: 构造下载请求失败: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qiniu: 下载失败: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("qiniu: 下载失败，HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// stat 获取对象元信息，不存在时返回 storage.ErrFileNotFound
func (fs *QiniuFileSystem) stat(ctx context.Context, key string) (qiniustorage.FileInfo, error) {
	info, err := fs.manager.Stat(fs.config.Bucket, key)
	if err != nil {
		if isQiniuNotFound(err) {
			return qiniustorage.FileInfo{}, storage.ErrFileNotFound
		}
		return qiniustorage.FileInfo{}, fmt.Errorf("qiniu: 获取文件信息失败: %w", err)
	}
	return info, nil
}

// fileFromStat 用 Stat 结果构造 File
func (fs *QiniuFileSystem) fileFromStat(key string, info qiniustorage.FileInfo) storage.File {
	return &qiniuFile{
		fs:       fs,
		key:      key,
		name:     path.Base(key),
		size:     info.Fsize,
		modTime:  qiniuPutTime(info.PutTime),
		mimeType: info.MimeType,
		meta: map[string]interface{}{
			"hash":     info.Hash,
			"md5":      info.Md5,
			"type":     info.Type,
			"putTime":  info.PutTime,
			"mimeType": info.MimeType,
		},
	}
}

// fileFromListItem 用列举结果构造 File
func (fs *QiniuFileSystem) fileFromListItem(item qiniustorage.ListItem) storage.File {
	return &qiniuFile{
		fs:       fs,
		key:      item.Key,
		name:     path.Base(item.Key),
		size:     item.Fsize,
		modTime:  qiniuPutTime(item.PutTime),
		mimeType: item.MimeType,
		meta: map[string]interface{}{
			"hash":     item.Hash,
			"type":     item.Type,
			"putTime":  item.PutTime,
			"mimeType": item.MimeType,
		},
	}
}

// list 分页列举对象；delimiter 为空表示递归列举
func (fs *QiniuFileSystem) list(ctx context.Context, prefix, delimiter string) ([]qiniustorage.ListItem, []string, error) {
	var (
		items      []qiniustorage.ListItem
		commonDirs []string
		marker     string
	)
	for {
		ret, hasNext, err := fs.manager.ListFilesWithContext(
			ctx,
			fs.config.Bucket,
			qiniustorage.ListInputOptionsPrefix(prefix),
			qiniustorage.ListInputOptionsDelimiter(delimiter),
			qiniustorage.ListInputOptionsMarker(marker),
			qiniustorage.ListInputOptionsLimit(1000),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("qiniu: 列举文件失败: %w", err)
		}
		items = append(items, ret.Items...)
		commonDirs = append(commonDirs, ret.CommonPrefixes...)
		if !hasNext || ret.Marker == "" {
			break
		}
		marker = ret.Marker
	}
	return items, commonDirs, nil
}

// Get 实现 storage.FileSystem
func (fs *QiniuFileSystem) Get(ctx context.Context, key string) (storage.File, error) {
	key = qiniuKey(key)
	if key == "" {
		return nil, storage.ErrInvalidPath
	}
	if strings.HasSuffix(key, "/") {
		return &qiniuFile{
			fs:    fs,
			key:   key,
			name:  path.Base(strings.TrimSuffix(key, "/")),
			isDir: true,
		}, nil
	}
	info, err := fs.stat(ctx, key)
	if err != nil {
		return nil, err
	}
	return fs.fileFromStat(key, info), nil
}

// Exists 实现 storage.FileSystem
func (fs *QiniuFileSystem) Exists(ctx context.Context, key string) (bool, error) {
	key = qiniuKey(key)
	if key == "" {
		return false, nil
	}
	if _, err := fs.stat(ctx, key); err != nil {
		if errors.Is(err, storage.ErrFileNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Write 实现 storage.FileSystem
func (fs *QiniuFileSystem) Write(ctx context.Context, key string, content []byte, options ...storage.WriteOption) error {
	return fs.WriteStream(ctx, key, bytes.NewReader(content), options...)
}

// WriteStream 实现 storage.FileSystem（七牛分片/断点续传上传）
func (fs *QiniuFileSystem) WriteStream(ctx context.Context, key string, content io.Reader, options ...storage.WriteOption) error {
	key = qiniuKey(key)
	if key == "" {
		return storage.ErrInvalidPath
	}

	opts := storage.DefaultWriteOptions()
	for _, option := range options {
		option(opts)
	}

	// 七牛没有对象级 ACL：显式要求 public 时必须使用公开空间，否则直接拒绝，
	// 避免「上传成功但链接 403」的静默失败。
	if opts.Visibility == "public" {
		private, err := fs.bucketPrivate()
		if err != nil {
			return err
		}
		if private {
			return storage.ErrVisibilityNotSupported
		}
	}

	if !opts.Overwrite {
		exists, err := fs.Exists(ctx, key)
		if err != nil {
			return err
		}
		if exists {
			return storage.ErrFileAlreadyExists
		}
	}

	mimeType := opts.MimeType
	if mimeType == "" {
		if ext := filepath.Ext(key); ext != "" {
			mimeType = mime.TypeByExtension(ext)
		}
	}

	putPolicy := qiniustorage.PutPolicy{Scope: fs.config.Bucket}
	uptoken := putPolicy.UploadToken(fs.mac)
	uploader := qiniustorage.NewResumeUploader(&qiniustorage.Config{})
	ret := qiniustorage.PutRet{}
	extra := qiniustorage.RputExtra{MimeType: mimeType}
	if err := uploader.PutWithoutSize(ctx, &ret, uptoken, key, content, &extra); err != nil {
		return fmt.Errorf("qiniu: 上传失败: %w", err)
	}
	return nil
}

// Delete 实现 storage.FileSystem
func (fs *QiniuFileSystem) Delete(ctx context.Context, key string) error {
	key = qiniuKey(key)
	if key == "" {
		return storage.ErrInvalidPath
	}
	if err := fs.manager.Delete(fs.config.Bucket, key); err != nil {
		if isQiniuNotFound(err) {
			return storage.ErrFileNotFound
		}
		return fmt.Errorf("qiniu: 删除失败: %w", err)
	}
	return nil
}

// DeleteDirectory 实现 storage.FileSystem（七牛没有目录，按前缀递归删除）
func (fs *QiniuFileSystem) DeleteDirectory(ctx context.Context, directory string) error {
	prefix := qiniuDirPrefix(directory)
	if prefix == "" {
		return errors.New("qiniu: 拒绝删除空目录（会清空整个空间）")
	}
	items, _, err := fs.list(ctx, prefix, "")
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := fs.manager.Delete(fs.config.Bucket, item.Key); err != nil && !isQiniuNotFound(err) {
			return fmt.Errorf("qiniu: 删除 %s 失败: %w", item.Key, err)
		}
	}
	return nil
}

// CreateDirectory 实现 storage.FileSystem；七牛没有目录概念，空实现
func (fs *QiniuFileSystem) CreateDirectory(ctx context.Context, directory string, options ...storage.WriteOption) error {
	return nil
}

// Files 实现 storage.FileSystem（只列当前层）
func (fs *QiniuFileSystem) Files(ctx context.Context, directory string) ([]storage.File, error) {
	prefix := qiniuDirPrefix(directory)
	items, _, err := fs.list(ctx, prefix, "/")
	if err != nil {
		return nil, err
	}
	files := make([]storage.File, 0, len(items))
	for _, item := range items {
		files = append(files, fs.fileFromListItem(item))
	}
	return files, nil
}

// AllFiles 实现 storage.FileSystem（递归列出所有文件）
func (fs *QiniuFileSystem) AllFiles(ctx context.Context, directory string) ([]storage.File, error) {
	prefix := qiniuDirPrefix(directory)
	items, _, err := fs.list(ctx, prefix, "")
	if err != nil {
		return nil, err
	}
	files := make([]storage.File, 0, len(items))
	for _, item := range items {
		files = append(files, fs.fileFromListItem(item))
	}
	return files, nil
}

// Directories 实现 storage.FileSystem（返回相对目录名）
func (fs *QiniuFileSystem) Directories(ctx context.Context, directory string) ([]string, error) {
	prefix := qiniuDirPrefix(directory)
	_, commonDirs, err := fs.list(ctx, prefix, "/")
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(commonDirs))
	for _, item := range commonDirs {
		name := strings.TrimPrefix(item, prefix)
		name = strings.TrimSuffix(name, "/")
		if name != "" {
			dirs = append(dirs, name)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// AllDirectories 实现 storage.FileSystem（递归返回相对目录名）
func (fs *QiniuFileSystem) AllDirectories(ctx context.Context, directory string) ([]string, error) {
	prefix := qiniuDirPrefix(directory)
	items, _, err := fs.list(ctx, prefix, "")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	dirs := make([]string, 0)
	for _, item := range items {
		relative := strings.TrimPrefix(item.Key, prefix)
		idx := strings.LastIndex(relative, "/")
		if idx <= 0 {
			continue
		}
		dir := relative[:idx]
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// Copy 实现 storage.FileSystem
func (fs *QiniuFileSystem) Copy(ctx context.Context, source, destination string) error {
	source = qiniuKey(source)
	destination = qiniuKey(destination)
	if source == "" || destination == "" {
		return storage.ErrInvalidPath
	}
	if err := fs.manager.Copy(fs.config.Bucket, source, fs.config.Bucket, destination, true); err != nil {
		if isQiniuNotFound(err) {
			return storage.ErrFileNotFound
		}
		return fmt.Errorf("qiniu: 复制失败: %w", err)
	}
	return nil
}

// Move 实现 storage.FileSystem
func (fs *QiniuFileSystem) Move(ctx context.Context, source, destination string) error {
	source = qiniuKey(source)
	destination = qiniuKey(destination)
	if source == "" || destination == "" {
		return storage.ErrInvalidPath
	}
	if err := fs.manager.Move(fs.config.Bucket, source, fs.config.Bucket, destination, true); err != nil {
		if isQiniuNotFound(err) {
			return storage.ErrFileNotFound
		}
		return fmt.Errorf("qiniu: 移动失败: %w", err)
	}
	return nil
}

// Size 实现 storage.FileSystem
func (fs *QiniuFileSystem) Size(ctx context.Context, key string) (int64, error) {
	info, err := fs.stat(ctx, qiniuKey(key))
	if err != nil {
		return 0, err
	}
	return info.Fsize, nil
}

// LastModified 实现 storage.FileSystem
func (fs *QiniuFileSystem) LastModified(ctx context.Context, key string) (time.Time, error) {
	info, err := fs.stat(ctx, qiniuKey(key))
	if err != nil {
		return time.Time{}, err
	}
	return qiniuPutTime(info.PutTime), nil
}

// MimeType 实现 storage.FileSystem
func (fs *QiniuFileSystem) MimeType(ctx context.Context, key string) (string, error) {
	info, err := fs.stat(ctx, qiniuKey(key))
	if err != nil {
		return "", err
	}
	return info.MimeType, nil
}

// SetVisibility 实现 storage.FileSystem。
// 七牛只有空间级 ACL，无法对单对象设置可见性；公开资源请使用独立的公开空间。
func (fs *QiniuFileSystem) SetVisibility(ctx context.Context, key, visibility string) error {
	return storage.ErrVisibilityNotSupported
}

// Visibility 实现 storage.FileSystem（返回空间级可见性）
func (fs *QiniuFileSystem) Visibility(ctx context.Context, key string) (string, error) {
	private, err := fs.bucketPrivate()
	if err != nil {
		return "", err
	}
	if private {
		return "private", nil
	}
	return "public", nil
}

// URL 实现 storage.FileSystem
func (fs *QiniuFileSystem) URL(ctx context.Context, key string) string {
	domain, err := fs.domain()
	if err != nil {
		return ""
	}
	return qiniustorage.MakePublicURLv2(domain, qiniuKey(key))
}

// TemporaryURL 实现 storage.FileSystem（七牛私有下载凭证；公开空间也可用）
func (fs *QiniuFileSystem) TemporaryURL(ctx context.Context, key string, expiration time.Duration) (string, error) {
	domain, err := fs.domain()
	if err != nil {
		return "", err
	}
	if expiration <= 0 {
		expiration = time.Hour
	}
	deadline := time.Now().Add(expiration).Unix()
	return qiniustorage.MakePrivateURLv2(fs.mac, domain, qiniuKey(key), deadline), nil
}

// Checksum 实现 storage.FileSystem。
// 七牛对象只提供 qetag（hash）与可选的 md5，其他算法需要下载内容后自行计算。
func (fs *QiniuFileSystem) Checksum(ctx context.Context, key, algorithm string) (string, error) {
	info, err := fs.stat(ctx, qiniuKey(key))
	if err != nil {
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(algorithm)) {
	case "", "etag", "qetag", "hash":
		return info.Hash, nil
	case "md5":
		if info.Md5 == "" {
			return "", fmt.Errorf("qiniu: 对象 %s 没有可用的 md5（归档存储或未计算）", key)
		}
		return info.Md5, nil
	default:
		return "", fmt.Errorf("qiniu: 不支持的校验算法 %q", algorithm)
	}
}

// qiniuKey 规整对象 key：统一正斜杠并去掉前导斜杠
func qiniuKey(key string) string {
	return strings.TrimLeft(normalizePath(key), "/")
}

// qiniuDirPrefix 规整目录前缀（去掉前导斜杠并补尾部斜杠）
func qiniuDirPrefix(directory string) string {
	prefix := qiniuKey(directory)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix
}

// qiniuFile 实现 storage.File
type qiniuFile struct {
	fs       *QiniuFileSystem
	key      string
	name     string
	size     int64
	modTime  time.Time
	mimeType string
	isDir    bool
	meta     map[string]interface{}
}

var _ storage.File = (*qiniuFile)(nil)

func (f *qiniuFile) Path() string            { return f.key }
func (f *qiniuFile) Name() string            { return f.name }
func (f *qiniuFile) Extension() string       { return path.Ext(f.key) }
func (f *qiniuFile) Size() int64             { return f.size }
func (f *qiniuFile) LastModified() time.Time { return f.modTime }
func (f *qiniuFile) IsDirectory() bool       { return f.isDir }
func (f *qiniuFile) MimeType() string        { return f.mimeType }

// Read 实现 storage.File
func (f *qiniuFile) Read(ctx context.Context) ([]byte, error) {
	stream, err := f.ReadStream(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	return io.ReadAll(stream)
}

// ReadStream 实现 storage.File
func (f *qiniuFile) ReadStream(ctx context.Context) (io.ReadCloser, error) {
	if f.isDir {
		return nil, fmt.Errorf("qiniu: %s 是目录，不能读取", f.key)
	}
	return f.fs.openStream(ctx, f.key)
}

// Visibility 实现 storage.File
func (f *qiniuFile) Visibility() string {
	visibility, err := f.fs.Visibility(context.Background(), f.key)
	if err != nil {
		return "private"
	}
	return visibility
}

// URL 实现 storage.File
func (f *qiniuFile) URL() string {
	return f.fs.URL(context.Background(), f.key)
}

// TemporaryURL 实现 storage.File
func (f *qiniuFile) TemporaryURL(ctx context.Context, expiration time.Duration) (string, error) {
	return f.fs.TemporaryURL(ctx, f.key, expiration)
}

// Metadata 实现 storage.File
func (f *qiniuFile) Metadata() map[string]interface{} {
	out := make(map[string]interface{}, len(f.meta))
	for key, value := range f.meta {
		out[key] = value
	}
	return out
}

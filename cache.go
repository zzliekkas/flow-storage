package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Cache 缓存接口
type Cache interface {
	// Get 从缓存获取值
	Get(ctx context.Context, key string, value interface{}) (bool, error)

	// Set 设置缓存值
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error

	// Forget 删除缓存项
	Forget(ctx context.Context, key string) error

	// Flush 清空所有缓存
	Flush(ctx context.Context) error

	// FlushByPattern 按模式清空缓存
	FlushByPattern(ctx context.Context, pattern string) error
}

// MemoryCache 内存缓存实现
type MemoryCache struct {
	items map[string]*cacheItem
	mu    sync.RWMutex
}

type cacheItem struct {
	value      interface{}
	expiration time.Time
}

// NewMemory 创建新的内存缓存
func NewMemory() Cache {
	return &MemoryCache{
		items: make(map[string]*cacheItem),
		mu:    sync.RWMutex{},
	}
}

// Get 从缓存获取值
func (c *MemoryCache) Get(ctx context.Context, key string, value interface{}) (bool, error) {
	c.mu.RLock()
	item, found := c.items[key]
	c.mu.RUnlock()

	if !found {
		return false, nil
	}

	// 检查是否过期
	if !item.expiration.IsZero() && item.expiration.Before(time.Now()) {
		c.Forget(ctx, key)
		return false, nil
	}

	// 简单赋值实现，实际应使用反射或JSON序列化/反序列化
	switch v := value.(type) {
	case *string:
		if str, ok := item.value.(string); ok {
			*v = str
			return true, nil
		}
	case *int:
		if i, ok := item.value.(int); ok {
			*v = i
			return true, nil
		}
	case *int64:
		if i, ok := item.value.(int64); ok {
			*v = i
			return true, nil
		}
	case *bool:
		if b, ok := item.value.(bool); ok {
			*v = b
			return true, nil
		}
	case *[]byte:
		if b, ok := item.value.([]byte); ok {
			*v = b
			return true, nil
		}
	case *map[string]interface{}:
		if m, ok := item.value.(map[string]interface{}); ok {
			*v = m
			return true, nil
		}
	case *FileInfo:
		if fi, ok := item.value.(*FileInfo); ok {
			*v = *fi
			return true, nil
		}
	}

	return false, nil
}

// Set 设置缓存值
func (c *MemoryCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	var expiration time.Time
	if ttl > 0 {
		expiration = time.Now().Add(ttl)
	}

	c.mu.Lock()
	c.items[key] = &cacheItem{
		value:      value,
		expiration: expiration,
	}
	c.mu.Unlock()

	return nil
}

// Forget 删除缓存项
func (c *MemoryCache) Forget(ctx context.Context, key string) error {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()

	return nil
}

// Flush 清空所有缓存
func (c *MemoryCache) Flush(ctx context.Context) error {
	c.mu.Lock()
	c.items = make(map[string]*cacheItem)
	c.mu.Unlock()

	return nil
}

// FlushByPattern 按模式清空缓存
func (c *MemoryCache) FlushByPattern(ctx context.Context, pattern string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 简单实现，仅支持前缀匹配
	for key := range c.items {
		if pattern[len(pattern)-1] == '*' && len(key) >= len(pattern)-1 {
			prefix := pattern[:len(pattern)-1]
			if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
				delete(c.items, key)
			}
		}
	}

	return nil
}

// CacheManager 文件缓存管理器
type CacheManager struct {
	// 存储管理器
	storage *Manager

	// 缓存实例
	cache Cache

	// 缓存前缀
	prefix string

	// 默认过期时间
	defaultTTL time.Duration

	// 缓存目录
	cacheDir string

	// 本地缓存锁
	mutex sync.RWMutex

	// 是否开启缓存
	enabled bool
}

// CacheOptions 缓存选项
type CacheOptions struct {
	// 缓存实例
	Cache Cache

	// 缓存前缀
	Prefix string

	// 默认过期时间
	DefaultTTL time.Duration

	// 缓存目录（用于大文件本地缓存）
	CacheDir string

	// 是否开启缓存
	Enabled bool
}

// NewCacheManager 创建文件缓存管理器
func NewCacheManager(manager *Manager, options *CacheOptions) *CacheManager {
	if options == nil {
		options = &CacheOptions{
			Prefix:     "storage:",
			DefaultTTL: 30 * time.Minute,
			CacheDir:   filepath.Join(os.TempDir(), "flow_storage_cache"),
			Enabled:    true,
		}
	}

	// 如果没有提供缓存实例，创建内存缓存
	if options.Cache == nil {
		options.Cache = NewMemory()
	}

	// 确保缓存目录存在
	if options.CacheDir != "" {
		os.MkdirAll(options.CacheDir, 0755)
	}

	return &CacheManager{
		storage:    manager,
		cache:      options.Cache,
		prefix:     options.Prefix,
		defaultTTL: options.DefaultTTL,
		cacheDir:   options.CacheDir,
		enabled:    options.Enabled,
		mutex:      sync.RWMutex{},
	}
}

// GetFile 获取文件（带缓存）
func (cm *CacheManager) GetFile(ctx context.Context, disk, path string) (File, error) {
	if !cm.enabled {
		return cm.getFromStorage(ctx, disk, path)
	}

	cacheKey := cm.getCacheKey(disk, path, "file")

	// 尝试从缓存获取
	var fileInfo *FileInfo
	found, err := cm.cache.Get(ctx, cacheKey, &fileInfo)
	if err == nil && found && fileInfo != nil {
		// 从缓存中找到文件信息
		return cm.createCachedFile(ctx, disk, path, fileInfo)
	}

	// 从存储中获取
	file, err := cm.getFromStorage(ctx, disk, path)
	if err != nil {
		return nil, err
	}

	// 缓存文件信息
	fileInfo = &FileInfo{
		FilePath:       file.Path(),
		FileName:       file.Name(),
		FileSize:       file.Size(),
		IsDir:          file.IsDirectory(),
		ModTime:        file.LastModified(),
		ContentType:    file.MimeType(),
		FileVisibility: file.Visibility(),
		MetaData:       file.Metadata(),
	}

	cm.cache.Set(ctx, cacheKey, fileInfo, cm.defaultTTL)

	return file, nil
}

// GetContent 获取文件内容（带缓存）
func (cm *CacheManager) GetContent(ctx context.Context, disk, path string) ([]byte, error) {
	if !cm.enabled {
		return cm.getContentFromStorage(ctx, disk, path)
	}

	cacheKey := cm.getCacheKey(disk, path, "content")

	// 对于小文件，直接从缓存获取
	var content []byte
	found, err := cm.cache.Get(ctx, cacheKey, &content)
	if err == nil && found && content != nil {
		return content, nil
	}

	// 从存储中获取
	content, err = cm.getContentFromStorage(ctx, disk, path)
	if err != nil {
		return nil, err
	}

	// 缓存文件内容（对于小文件）
	if len(content) <= 1024*1024 { // 小于1MB的文件
		cm.cache.Set(ctx, cacheKey, content, cm.defaultTTL)
	} else {
		// 对于大文件，缓存到本地文件系统
		if cm.cacheDir != "" {
			localPath := cm.saveToLocalCache(disk, path, content)
			// 缓存本地路径
			cm.cache.Set(ctx, cacheKey+":local", localPath, cm.defaultTTL)
		}
	}

	return content, nil
}

// GetStream 获取文件流（带缓存）
func (cm *CacheManager) GetStream(ctx context.Context, disk, path string) (io.ReadCloser, error) {
	if !cm.enabled {
		return cm.getStreamFromStorage(ctx, disk, path)
	}

	cacheKey := cm.getCacheKey(disk, path, "content")

	// 尝试从本地缓存获取
	var localPath string
	found, err := cm.cache.Get(ctx, cacheKey+":local", &localPath)
	if err == nil && found && localPath != "" {
		// 尝试从本地文件打开
		file, err := os.Open(localPath)
		if err == nil {
			return file, nil
		}
	}

	// 尝试从内存缓存获取
	var content []byte
	found, err = cm.cache.Get(ctx, cacheKey, &content)
	if err == nil && found && content != nil {
		return io.NopCloser(io.NewSectionReader(newBytesReader(content), 0, int64(len(content)))), nil
	}

	// 从存储中获取
	stream, err := cm.getStreamFromStorage(ctx, disk, path)
	if err != nil {
		return nil, err
	}

	// 对于流，我们不能直接缓存，但可以缓存内容
	// 这里返回原始流，如果需要缓存，应该使用GetContent
	return stream, nil
}

// Exists 检查文件是否存在（带缓存）
func (cm *CacheManager) Exists(ctx context.Context, disk, path string) (bool, error) {
	if !cm.enabled {
		return cm.existsInStorage(ctx, disk, path)
	}

	cacheKey := cm.getCacheKey(disk, path, "exists")

	// 尝试从缓存获取
	var exists bool
	found, err := cm.cache.Get(ctx, cacheKey, &exists)
	if err == nil && found {
		return exists, nil
	}

	// 从存储中检查
	exists, err = cm.existsInStorage(ctx, disk, path)
	if err != nil {
		return false, err
	}

	// 缓存结果
	cm.cache.Set(ctx, cacheKey, exists, cm.defaultTTL)

	return exists, nil
}

// InvalidateCache 使缓存失效
func (cm *CacheManager) InvalidateCache(ctx context.Context, disk, path string) error {
	if !cm.enabled {
		return nil
	}

	// 移除所有相关缓存
	fileKey := cm.getCacheKey(disk, path, "file")
	contentKey := cm.getCacheKey(disk, path, "content")
	existsKey := cm.getCacheKey(disk, path, "exists")
	localKey := contentKey + ":local"

	// 检查是否有本地缓存
	var localPath string
	found, _ := cm.cache.Get(ctx, localKey, &localPath)
	if found && localPath != "" {
		// 尝试删除本地缓存文件
		os.Remove(localPath)
	}

	// 删除缓存键
	cm.cache.Forget(ctx, fileKey)
	cm.cache.Forget(ctx, contentKey)
	cm.cache.Forget(ctx, existsKey)
	cm.cache.Forget(ctx, localKey)

	return nil
}

// InvalidateCacheByPattern 按模式使缓存失效
func (cm *CacheManager) InvalidateCacheByPattern(ctx context.Context, disk, pattern string) error {
	if !cm.enabled {
		return nil
	}

	// 创建匹配前缀
	prefix := cm.prefix + disk + ":"
	if pattern != "" {
		prefix += pattern
	}

	// 清除匹配的缓存
	return cm.cache.FlushByPattern(ctx, prefix+"*")
}

// SetEnabled 设置是否启用缓存
func (cm *CacheManager) SetEnabled(enabled bool) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.enabled = enabled
}

// IsEnabled 检查缓存是否启用
func (cm *CacheManager) IsEnabled() bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.enabled
}

// Flush 清空所有缓存
func (cm *CacheManager) Flush(ctx context.Context) error {
	if !cm.enabled {
		return nil
	}

	// 清除所有缓存
	if err := cm.cache.Flush(ctx); err != nil {
		return err
	}

	// 清除本地缓存目录
	if cm.cacheDir != "" {
		// 仅删除目录内容，保留目录
		entries, err := os.ReadDir(cm.cacheDir)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			os.RemoveAll(filepath.Join(cm.cacheDir, entry.Name()))
		}
	}

	return nil
}

// 内部助手方法

// getFromStorage 从存储中获取文件
func (cm *CacheManager) getFromStorage(ctx context.Context, disk, path string) (File, error) {
	var fs FileSystem
	var err error

	if disk == "" {
		fs, err = cm.storage.DefaultDisk()
	} else {
		fs, err = cm.storage.Disk(disk)
	}

	if err != nil {
		return nil, err
	}

	return fs.Get(ctx, path)
}

// getContentFromStorage 从存储中获取文件内容
func (cm *CacheManager) getContentFromStorage(ctx context.Context, disk, path string) ([]byte, error) {
	file, err := cm.getFromStorage(ctx, disk, path)
	if err != nil {
		return nil, err
	}

	return file.Read(ctx)
}

// getStreamFromStorage 从存储中获取文件流
func (cm *CacheManager) getStreamFromStorage(ctx context.Context, disk, path string) (io.ReadCloser, error) {
	file, err := cm.getFromStorage(ctx, disk, path)
	if err != nil {
		return nil, err
	}

	return file.ReadStream(ctx)
}

// existsInStorage 检查文件是否存在于存储中
func (cm *CacheManager) existsInStorage(ctx context.Context, disk, path string) (bool, error) {
	var fs FileSystem
	var err error

	if disk == "" {
		fs, err = cm.storage.DefaultDisk()
	} else {
		fs, err = cm.storage.Disk(disk)
	}

	if err != nil {
		return false, err
	}

	return fs.Exists(ctx, path)
}

// getCacheKey 生成缓存键
func (cm *CacheManager) getCacheKey(disk, path, type_ string) string {
	if disk == "" {
		disk = cm.storage.GetDefaultDiskName()
	}
	return cm.prefix + disk + ":" + path + ":" + type_
}

// saveToLocalCache 保存文件到本地缓存
func (cm *CacheManager) saveToLocalCache(disk, path string, content []byte) string {
	hash := md5.Sum([]byte(disk + ":" + path))
	hashStr := hex.EncodeToString(hash[:])

	ext := filepath.Ext(path)
	filename := hashStr
	if ext != "" {
		filename += ext
	}

	localPath := filepath.Join(cm.cacheDir, filename)

	// 保存文件
	os.WriteFile(localPath, content, 0644)

	return localPath
}

// createCachedFile 从缓存的信息创建文件对象
func (cm *CacheManager) createCachedFile(ctx context.Context, disk, path string, info *FileInfo) (File, error) {
	// 这里我们需要创建一个实现File接口的对象
	// 对于真正的内容访问，我们还是需要走缓存或存储
	return &CachedFile{
		fileInfo: info,
		cacheMgr: cm,
		disk:     disk,
		path:     path,
		ctx:      ctx,
	}, nil
}

// CachedFile 表示一个缓存的文件
type CachedFile struct {
	fileInfo *FileInfo
	cacheMgr *CacheManager
	disk     string
	path     string
	ctx      context.Context
}

// Path 返回文件的路径
func (f *CachedFile) Path() string {
	return f.fileInfo.FilePath
}

// Name 返回文件的名称
func (f *CachedFile) Name() string {
	return f.fileInfo.FileName
}

// Extension 返回文件的扩展名
func (f *CachedFile) Extension() string {
	return filepath.Ext(f.fileInfo.FileName)
}

// Size 返回文件的大小
func (f *CachedFile) Size() int64 {
	return f.fileInfo.FileSize
}

// LastModified 返回文件的最后修改时间
func (f *CachedFile) LastModified() time.Time {
	return f.fileInfo.ModTime
}

// IsDirectory 判断是否为目录
func (f *CachedFile) IsDirectory() bool {
	return f.fileInfo.IsDir
}

// Read 读取文件内容
func (f *CachedFile) Read(ctx context.Context) ([]byte, error) {
	return f.cacheMgr.GetContent(ctx, f.disk, f.path)
}

// ReadStream 获取文件的读取流
func (f *CachedFile) ReadStream(ctx context.Context) (io.ReadCloser, error) {
	return f.cacheMgr.GetStream(ctx, f.disk, f.path)
}

// MimeType 返回文件的MIME类型
func (f *CachedFile) MimeType() string {
	return f.fileInfo.ContentType
}

// Visibility 返回文件的可见性
func (f *CachedFile) Visibility() string {
	return f.fileInfo.FileVisibility
}

// URL 获取文件的URL
func (f *CachedFile) URL() string {
	// URL需要从实际存储中获取
	fs, err := f.cacheMgr.storage.Disk(f.disk)
	if err != nil {
		return ""
	}
	return fs.URL(f.ctx, f.path)
}

// TemporaryURL 获取文件的临时URL
func (f *CachedFile) TemporaryURL(ctx context.Context, expiration time.Duration) (string, error) {
	// 临时URL需要从实际存储中获取
	fs, err := f.cacheMgr.storage.Disk(f.disk)
	if err != nil {
		return "", err
	}
	return fs.TemporaryURL(ctx, f.path, expiration)
}

// Metadata 获取文件的元数据
func (f *CachedFile) Metadata() map[string]interface{} {
	return f.fileInfo.MetaData
}

// bytesReader 实现ReaderAt接口的字节读取器
type bytesReader struct {
	bytes []byte
}

func newBytesReader(b []byte) *bytesReader {
	return &bytesReader{bytes: b}
}

func (r *bytesReader) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(r.bytes)) {
		return 0, io.EOF
	}
	n = copy(p, r.bytes[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

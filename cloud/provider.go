package cloud

import (
	"context"
	"errors"
	"fmt"
	"sync"

	storage "github.com/zzliekkas/flow-storage"
	"github.com/zzliekkas/flow-storage/core"
)

// Provider 云存储提供者接口
type Provider interface {
	// RegisterCloud 注册云存储驱动
	RegisterCloud(name string, fs core.FileSystem) error

	// UnregisterCloud 注销云存储驱动
	UnregisterCloud(name string) error

	// GetCloud 获取云存储驱动
	GetCloud(name string) (core.FileSystem, error)

	// HasCloud 检查云存储驱动是否存在
	HasCloud(name string) bool

	// GetCloudNames 获取所有已注册的云存储驱动名称
	GetCloudNames() []string
}

// CloudManager 云存储管理器
type CloudManager struct {
	// 存储所有已注册的云存储驱动
	filesystems map[string]core.FileSystem

	// 互斥锁，保证并发安全
	mu sync.RWMutex
}

// NewCloudManager 创建云存储管理器
func NewCloudManager() *CloudManager {
	return &CloudManager{
		filesystems: make(map[string]core.FileSystem),
		mu:          sync.RWMutex{},
	}
}

// RegisterCloud 注册云存储驱动
func (m *CloudManager) RegisterCloud(name string, fs core.FileSystem) error {
	if name == "" {
		return errors.New("cloud: 驱动名称不能为空")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.filesystems[name] = fs
	return nil
}

// UnregisterCloud 注销云存储驱动
func (m *CloudManager) UnregisterCloud(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filesystems[name]; !exists {
		return fmt.Errorf("cloud: 驱动 '%s' 不存在", name)
	}

	delete(m.filesystems, name)
	return nil
}

// GetCloud 获取云存储驱动
func (m *CloudManager) GetCloud(name string) (core.FileSystem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if fs, exists := m.filesystems[name]; exists {
		return fs, nil
	}

	return nil, fmt.Errorf("cloud: 驱动 '%s' 不存在", name)
}

// HasCloud 检查云存储驱动是否存在
func (m *CloudManager) HasCloud(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.filesystems[name]
	return exists
}

// GetCloudNames 获取所有已注册的云存储驱动名称
func (m *CloudManager) GetCloudNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.filesystems))
	for name := range m.filesystems {
		names = append(names, name)
	}
	return names
}

// RegisterToManager 将云存储驱动注册到存储管理器
func RegisterToManager(manager interface{}, cloudManager *CloudManager) error {
	if manager == nil {
		return errors.New("cloud: 存储管理器不能为空")
	}

	if cloudManager == nil {
		return errors.New("cloud: 云存储管理器不能为空")
	}

	// 使用接口断言检查manager是否实现RegisterDisk方法
	type DiskRegistrar interface {
		RegisterDisk(name string, fs interface{}) error
	}

	diskManager, ok := manager.(DiskRegistrar)
	if !ok {
		return fmt.Errorf("cloud: 管理器类型 %T 不实现RegisterDisk方法", manager)
	}

	// 注册所有云存储驱动到存储管理器
	for _, name := range cloudManager.GetCloudNames() {
		fs, err := cloudManager.GetCloud(name)
		if err != nil {
			return err
		}
		// 直接传递core.FileSystem，让storage.Manager内部处理转换
		if err := diskManager.RegisterDisk("cloud_"+name, fs); err != nil {
			return err
		}
	}

	return nil
}

// ResolveCloudDriver 根据驱动类型创建云存储驱动
func ResolveCloudDriver(ctx context.Context, driverType string, config map[string]interface{}) (core.FileSystem, error) {
	switch driverType {
	case "s3":
		return createS3Driver(config)
	case "oss":
		return createOSSDriver(config)
	case "cos":
		return createCOSDriver(config)
	case "gcs":
		return createGCSDriver(config)
	case "qiniu":
		return createQiniuDriver(config)
	default:
		return nil, fmt.Errorf("cloud: 不支持的驱动类型 '%s'", driverType)
	}
}

// 创建S3驱动
func createS3Driver(config map[string]interface{}) (core.FileSystem, error) {
	cfg := S3Config{
		Endpoint:          getStringValue(config, "endpoint", ""),
		Region:            getStringValue(config, "region", ""),
		Bucket:            getStringValue(config, "bucket", ""),
		AccessKey:         getStringValue(config, "access_key", ""),
		SecretKey:         getStringValue(config, "secret_key", ""),
		UseSSL:            getBoolValue(config, "use_ssl", true),
		PublicURL:         getStringValue(config, "public_url", ""),
		ForcePathStyle:    getBoolValue(config, "force_path_style", false),
		DefaultVisibility: getStringValue(config, "default_visibility", "private"),
	}

	return New(cfg)
}

// 创建OSS驱动
func createOSSDriver(config map[string]interface{}) (core.FileSystem, error) {
	cfg := OSSConfig{
		Endpoint:          getStringValue(config, "endpoint", ""),
		Bucket:            getStringValue(config, "bucket", ""),
		AccessKeyID:       getStringValue(config, "access_key_id", ""),
		AccessKeySecret:   getStringValue(config, "access_key_secret", ""),
		UseSSL:            getBoolValue(config, "use_ssl", true),
		PublicURL:         getStringValue(config, "public_url", ""),
		DefaultVisibility: getStringValue(config, "default_visibility", "private"),
	}

	fs, err := NewOSS(cfg)
	if err != nil {
		return nil, err
	}

	// 使用适配器将storage.FileSystem转换为core.FileSystem
	return &storage.StorageToCoreFSAdapter{StorageFS: fs}, nil
}

// 创建COS驱动（待实现）
func createCOSDriver(config map[string]interface{}) (core.FileSystem, error) {
	// 创建COS配置
	cfg := COSConfig{
		AppID:             getStringValue(config, "app_id", ""),
		SecretID:          getStringValue(config, "secret_id", ""),
		SecretKey:         getStringValue(config, "secret_key", ""),
		Bucket:            getStringValue(config, "bucket", ""),
		Region:            getStringValue(config, "region", ""),
		UseSSL:            getBoolValue(config, "use_ssl", true),
		PublicURL:         getStringValue(config, "public_url", ""),
		DefaultVisibility: getStringValue(config, "default_visibility", "private"),
	}

	// 设置URL过期时间（如果提供）
	if val, ok := config["url_expiry"]; ok {
		if intVal, ok := val.(int64); ok {
			cfg.UrlExpiry = intVal
		} else if floatVal, ok := val.(float64); ok {
			cfg.UrlExpiry = int64(floatVal)
		}
	}

	// 直接返回，COS驱动实现的是core.FileSystem接口
	return NewCOS(cfg)
}

// 创建GCS驱动（待实现）
func createGCSDriver(config map[string]interface{}) (core.FileSystem, error) {
	return nil, errors.New("cloud: GCS驱动尚未实现")
}

// 创建Qiniu驱动
func createQiniuDriver(config map[string]interface{}) (core.FileSystem, error) {
	cfg := QiniuConfig{
		AccessKey: getStringValue(config, "access_key", ""),
		SecretKey: getStringValue(config, "secret_key", ""),
		Bucket:    getStringValue(config, "bucket", ""),
		Domain:    getStringValue(config, "domain", ""),
	}
	fs, err := NewQiniu(cfg)
	if err != nil {
		return nil, err
	}
	// 用适配器包裹，兼容core.FileSystem接口
	return &storage.StorageToCoreFSAdapter{StorageFS: fs}, nil
}

// 辅助函数：从配置映射获取字符串值
func getStringValue(config map[string]interface{}, key, defaultValue string) string {
	if val, ok := config[key]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return defaultValue
}

// 辅助函数：从配置映射获取布尔值
func getBoolValue(config map[string]interface{}, key string, defaultValue bool) bool {
	if val, ok := config[key]; ok {
		if boolVal, ok := val.(bool); ok {
			return boolVal
		}
	}
	return defaultValue
}

func (fs *QiniuFileSystem) AllDirectories(ctx context.Context, directory string) ([]string, error) {
	return nil, nil
}

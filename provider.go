package storage

import (
	"fmt"
	"os"

	"github.com/zzliekkas/flow-storage/local"
)

// StorageConfig 文件存储系统配置
type StorageConfig struct {
	// 默认驱动
	DefaultDisk string `mapstructure:"default_disk"`

	// 磁盘配置
	Disks map[string]DiskConfig `mapstructure:"disks"`
}

// DiskConfig 磁盘配置
type DiskConfig struct {
	// 驱动类型 (local, s3, etc.)
	Driver string `mapstructure:"driver"`

	// 驱动配置
	Config map[string]interface{} `mapstructure:"config"`
}

// Provider 文件存储系统服务提供者
type Provider struct {
	config StorageConfig
}

// NewProvider 创建文件存储系统服务提供者
func NewProvider(config StorageConfig) *Provider {
	return &Provider{
		config: config,
	}
}

// DefaultConfig 返回默认配置
func DefaultConfig() StorageConfig {
	return StorageConfig{
		DefaultDisk: "local",
		Disks: map[string]DiskConfig{
			"local": {
				Driver: "local",
				Config: map[string]interface{}{
					"root":                  "./storage",
					"base_url":              "/storage",
					"file_permissions":      0644,
					"directory_permissions": 0755,
					"public_permissions":    0644,
					"private_permissions":   0600,
				},
			},
		},
	}
}

// Build 构建文件存储系统服务
func (p *Provider) Build() (*Manager, *Uploader, error) {
	manager := NewManager()

	for name, diskConfig := range p.config.Disks {
		disk, err := p.createDisk(diskConfig)
		if err != nil {
			return nil, nil, err
		}

		manager.RegisterDisk(name, disk)
	}

	if p.config.DefaultDisk != "" {
		if err := manager.SetDefaultDisk(p.config.DefaultDisk); err != nil {
			return nil, nil, err
		}
	}

	uploader := NewUploader(manager)
	return manager, uploader, nil
}

// createDisk 创建磁盘
func (p *Provider) createDisk(config DiskConfig) (FileSystem, error) {
	switch config.Driver {
	case "local":
		// 创建本地文件系统配置
		localConfig := local.DefaultConfig()

		// 应用配置
		if root, ok := config.Config["root"].(string); ok {
			localConfig.Root = root
		}
		if baseURL, ok := config.Config["base_url"].(string); ok {
			localConfig.BaseURL = baseURL
		}
		if filePerms, ok := config.Config["file_permissions"].(int); ok {
			localConfig.FilePermissions = os.FileMode(filePerms)
		}
		if dirPerms, ok := config.Config["directory_permissions"].(int); ok {
			localConfig.DirectoryPermissions = os.FileMode(dirPerms)
		}
		if publicPerms, ok := config.Config["public_permissions"].(int); ok {
			localConfig.PublicPermissions = os.FileMode(publicPerms)
		}
		if privatePerms, ok := config.Config["private_permissions"].(int); ok {
			localConfig.PrivatePermissions = os.FileMode(privatePerms)
		}

		// 创建本地文件系统驱动
		fs, err := local.New(localConfig)
		if err != nil {
			return nil, err
		}

		// 使用适配器将core.FileSystem转换为storage.FileSystem
		return &FileSystemAdapter{CoreFS: fs}, nil

	// 可以在这里添加更多的驱动类型
	// case "s3":
	//     return createS3Disk(config.Config)

	default:
		return nil, fmt.Errorf("不支持的驱动类型: %s", config.Driver)
	}
}

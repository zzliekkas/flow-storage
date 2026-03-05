package storage

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/zzliekkas/flow-storage/core"
)

// Manager 存储管理器
type Manager struct {
	// 已注册的磁盘驱动
	disks map[string]FileSystem

	// 默认磁盘名称
	defaultDisk string

	// 互斥锁保证并发安全
	mu sync.RWMutex
}

// NewManager 创建新的存储管理器
func NewManager() *Manager {
	return &Manager{
		disks: make(map[string]FileSystem),
		mu:    sync.RWMutex{},
	}
}

// Disk 获取指定名称的文件系统驱动
func (m *Manager) Disk(name string) (FileSystem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if fs, ok := m.disks[name]; ok {
		return fs, nil
	}

	return nil, fmt.Errorf("storage: 磁盘 '%s' 未找到", name)
}

// DefaultDisk 获取默认文件系统驱动
func (m *Manager) DefaultDisk() (FileSystem, error) {
	if m.defaultDisk == "" {
		return nil, fmt.Errorf("storage: 未设置默认磁盘")
	}
	return m.Disk(m.defaultDisk)
}

// SetDefaultDisk 设置默认文件系统驱动
func (m *Manager) SetDefaultDisk(name string) error {
	// 检查指定的磁盘是否存在
	if _, err := m.Disk(name); err != nil {
		return err
	}

	m.mu.Lock()
	m.defaultDisk = name
	m.mu.Unlock()

	return nil
}

// RegisterDisk 注册存储驱动
func (m *Manager) RegisterDisk(name string, fs interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 支持不同类型的文件系统注册
	switch v := fs.(type) {
	case FileSystem:
		// 如果已经是storage.FileSystem，直接使用
		m.disks[name] = v
	case core.FileSystem:
		// 如果是core.FileSystem，使用适配器转换
		m.disks[name] = &FileSystemAdapter{CoreFS: v}
	default:
		panic(fmt.Sprintf("不支持的文件系统类型: %T", fs))
	}

	// 如果是第一个注册的驱动，将其设为默认
	if m.defaultDisk == "" {
		m.defaultDisk = name
	}
}

// UnregisterDisk 注销文件系统驱动
func (m *Manager) UnregisterDisk(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.disks, name)

	// 如果移除的是默认磁盘，重新设置默认磁盘
	if m.defaultDisk == name {
		m.defaultDisk = ""
		// 如果还有其他磁盘，选择第一个作为默认磁盘
		for diskName := range m.disks {
			m.defaultDisk = diskName
			break
		}
	}
}

// GetDiskNames 获取所有已注册的文件系统驱动名称
func (m *Manager) GetDiskNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.disks))
	for name := range m.disks {
		names = append(names, name)
	}
	return names
}

// HasDisk 检查指定名称的文件系统驱动是否存在
func (m *Manager) HasDisk(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.disks[name]
	return exists
}

// GetDefaultDiskName 获取默认文件系统驱动名称
func (m *Manager) GetDefaultDiskName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.defaultDisk
}

// 以下方法是对默认文件系统驱动的操作代理

// Get 从默认磁盘获取文件
func (m *Manager) Get(ctx context.Context, path string) (File, error) {
	fs, err := m.DefaultDisk()
	if err != nil {
		return nil, err
	}
	return fs.Get(ctx, path)
}

// Exists 检查文件在默认磁盘上是否存在
func (m *Manager) Exists(ctx context.Context, path string) (bool, error) {
	fs, err := m.DefaultDisk()
	if err != nil {
		return false, err
	}
	return fs.Exists(ctx, path)
}

// Write 向默认磁盘写入文件
func (m *Manager) Write(ctx context.Context, path string, content []byte, options ...WriteOption) error {
	fs, err := m.DefaultDisk()
	if err != nil {
		return err
	}
	return fs.Write(ctx, path, content, options...)
}

// WriteStream 向默认磁盘通过流写入文件
func (m *Manager) WriteStream(ctx context.Context, path string, content interface{}, options ...WriteOption) error {
	// 类型转换
	reader, ok := content.(io.Reader)
	if !ok {
		return fmt.Errorf("content must implement io.Reader")
	}

	fs, err := m.DefaultDisk()
	if err != nil {
		return err
	}
	return fs.WriteStream(ctx, path, reader, options...)
}

// Delete 从默认磁盘删除文件
func (m *Manager) Delete(ctx context.Context, path string) error {
	fs, err := m.DefaultDisk()
	if err != nil {
		return err
	}
	return fs.Delete(ctx, path)
}

// DeleteDirectory 从默认磁盘删除目录
func (m *Manager) DeleteDirectory(ctx context.Context, path string) error {
	fs, err := m.DefaultDisk()
	if err != nil {
		return err
	}
	return fs.DeleteDirectory(ctx, path)
}

// CreateDirectory 在默认磁盘上创建目录
func (m *Manager) CreateDirectory(ctx context.Context, path string, options ...WriteOption) error {
	fs, err := m.DefaultDisk()
	if err != nil {
		return err
	}
	return fs.CreateDirectory(ctx, path, options...)
}

// Files 列出默认磁盘目录下的所有文件
func (m *Manager) Files(ctx context.Context, directory string) ([]File, error) {
	fs, err := m.DefaultDisk()
	if err != nil {
		return nil, err
	}
	return fs.Files(ctx, directory)
}

// AllFiles 递归列出默认磁盘目录下的所有文件
func (m *Manager) AllFiles(ctx context.Context, directory string) ([]File, error) {
	fs, err := m.DefaultDisk()
	if err != nil {
		return nil, err
	}
	return fs.AllFiles(ctx, directory)
}

// Directories 列出默认磁盘目录下的所有子目录
func (m *Manager) Directories(ctx context.Context, directory string) ([]string, error) {
	fs, err := m.DefaultDisk()
	if err != nil {
		return nil, err
	}
	return fs.Directories(ctx, directory)
}

// Copy 在默认磁盘上复制文件
func (m *Manager) Copy(ctx context.Context, source, destination string) error {
	fs, err := m.DefaultDisk()
	if err != nil {
		return err
	}
	return fs.Copy(ctx, source, destination)
}

// Move 在默认磁盘上移动文件
func (m *Manager) Move(ctx context.Context, source, destination string) error {
	fs, err := m.DefaultDisk()
	if err != nil {
		return err
	}
	return fs.Move(ctx, source, destination)
}

// URL 获取默认磁盘上文件的URL
func (m *Manager) URL(ctx context.Context, path string) (string, error) {
	fs, err := m.DefaultDisk()
	if err != nil {
		return "", err
	}
	return fs.URL(ctx, path), nil
}

// TemporaryURL 获取默认磁盘上文件的临时URL
func (m *Manager) TemporaryURL(ctx context.Context, path string, expiration time.Duration) (string, error) {
	fs, err := m.DefaultDisk()
	if err != nil {
		return "", err
	}
	return fs.TemporaryURL(ctx, path, expiration)
}

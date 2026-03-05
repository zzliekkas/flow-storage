package storage

import (
	"github.com/zzliekkas/flow/v2"
)

// StorageModule implements flow.Module for easy registration into a Flow engine.
type StorageModule struct {
	config StorageConfig
}

// NewModule creates a new StorageModule with the given configuration.
func NewModule(config StorageConfig) *StorageModule {
	return &StorageModule{config: config}
}

// Name returns the module name.
func (m *StorageModule) Name() string {
	return "storage"
}

// Init registers storage services into Flow's DI container.
func (m *StorageModule) Init(e *flow.Engine) error {
	provider := NewProvider(m.config)
	manager, uploader, err := provider.Build()
	if err != nil {
		return err
	}

	if err := e.Provide(func() *Manager { return manager }); err != nil {
		return err
	}
	if err := e.Provide(func() *Uploader { return uploader }); err != nil {
		return err
	}
	return nil
}

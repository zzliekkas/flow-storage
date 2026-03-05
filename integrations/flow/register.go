package flowintegration

import (
	storage "github.com/zzliekkas/flow-storage"
	"github.com/zzliekkas/flow/v2/di"
)

// Register builds storage services from the given config and registers them into Flow's DI container.
func Register(container *di.Container, cfg storage.StorageConfig) error {
	provider := storage.NewProvider(cfg)
	manager, uploader, err := provider.Build()
	if err != nil {
		return err
	}

	container.Provide(func() *storage.Manager {
		return manager
	})
	container.Provide(func() *storage.Uploader {
		return uploader
	})
	return nil
}

// RegisterWithProvider builds services using a prepared Provider instance and registers them into Flow's DI container.
func RegisterWithProvider(container *di.Container, provider *storage.Provider) error {
	manager, uploader, err := provider.Build()
	if err != nil {
		return err
	}

	container.Provide(func() *storage.Manager {
		return manager
	})
	container.Provide(func() *storage.Uploader {
		return uploader
	})
	return nil
}

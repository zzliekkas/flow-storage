package storage

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/zzliekkas/flow-storage/v3/core"
)

// FileAdapter 适配器，用于将core.File转换为storage.File
type FileAdapter struct {
	CoreFile core.File
}

// 实现File接口
func (f *FileAdapter) Path() string {
	return f.CoreFile.Path()
}

func (f *FileAdapter) Name() string {
	return f.CoreFile.Name()
}

func (f *FileAdapter) Extension() string {
	return f.CoreFile.Extension()
}

func (f *FileAdapter) Size() int64 {
	return f.CoreFile.Size()
}

func (f *FileAdapter) LastModified() time.Time {
	return f.CoreFile.LastModified()
}

func (f *FileAdapter) IsDirectory() bool {
	return f.CoreFile.IsDirectory()
}

func (f *FileAdapter) Read(ctx context.Context) ([]byte, error) {
	return f.CoreFile.Read(ctx)
}

func (f *FileAdapter) ReadStream(ctx context.Context) (io.ReadCloser, error) {
	return f.CoreFile.ReadStream(ctx)
}

func (f *FileAdapter) MimeType() string {
	return f.CoreFile.MimeType()
}

func (f *FileAdapter) Visibility() string {
	return f.CoreFile.Visibility()
}

func (f *FileAdapter) URL() string {
	return f.CoreFile.URL()
}

func (f *FileAdapter) TemporaryURL(ctx context.Context, expiration time.Duration) (string, error) {
	return f.CoreFile.TemporaryURL(ctx, expiration)
}

func (f *FileAdapter) Metadata() map[string]interface{} {
	return f.CoreFile.Metadata()
}

// FileSystemAdapter 适配器，用于将core.FileSystem转换为storage.FileSystem
type FileSystemAdapter struct {
	CoreFS core.FileSystem
}

// 实现FileSystem接口
func (fs *FileSystemAdapter) Get(ctx context.Context, path string) (File, error) {
	file, err := fs.CoreFS.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	return &FileAdapter{CoreFile: file}, nil
}

func (fs *FileSystemAdapter) Exists(ctx context.Context, path string) (bool, error) {
	return fs.CoreFS.Exists(ctx, path)
}

func (fs *FileSystemAdapter) Write(ctx context.Context, path string, content []byte, options ...WriteOption) error {
	// 将storage.WriteOption转换为core.WriteOption
	coreOptions := ConvertToCoreWriteOptions(options)
	return fs.CoreFS.Write(ctx, path, content, coreOptions...)
}

func (fs *FileSystemAdapter) WriteStream(ctx context.Context, path string, content io.Reader, options ...WriteOption) error {
	// 将storage.WriteOption转换为core.WriteOption
	coreOptions := ConvertToCoreWriteOptions(options)
	return fs.CoreFS.WriteStream(ctx, path, content, coreOptions...)
}

func (fs *FileSystemAdapter) Delete(ctx context.Context, path string) error {
	return fs.CoreFS.Delete(ctx, path)
}

func (fs *FileSystemAdapter) DeleteDirectory(ctx context.Context, path string) error {
	return fs.CoreFS.DeleteDirectory(ctx, path)
}

func (fs *FileSystemAdapter) CreateDirectory(ctx context.Context, path string, options ...WriteOption) error {
	// 将storage.WriteOption转换为core.WriteOption
	coreOptions := ConvertToCoreWriteOptions(options)
	return fs.CoreFS.CreateDirectory(ctx, path, coreOptions...)
}

func (fs *FileSystemAdapter) Files(ctx context.Context, directory string) ([]File, error) {
	coreFiles, err := fs.CoreFS.Files(ctx, directory)
	if err != nil {
		return nil, err
	}

	// 转换core.File数组为File数组
	files := make([]File, len(coreFiles))
	for i, file := range coreFiles {
		files[i] = &FileAdapter{CoreFile: file}
	}
	return files, nil
}

func (fs *FileSystemAdapter) AllFiles(ctx context.Context, directory string) ([]File, error) {
	coreFiles, err := fs.CoreFS.AllFiles(ctx, directory)
	if err != nil {
		return nil, err
	}

	// 转换core.File数组为File数组
	files := make([]File, len(coreFiles))
	for i, file := range coreFiles {
		files[i] = &FileAdapter{CoreFile: file}
	}
	return files, nil
}

func (fs *FileSystemAdapter) Directories(ctx context.Context, directory string) ([]string, error) {
	return fs.CoreFS.Directories(ctx, directory)
}

func (fs *FileSystemAdapter) AllDirectories(ctx context.Context, directory string) ([]string, error) {
	return fs.CoreFS.AllDirectories(ctx, directory)
}

func (fs *FileSystemAdapter) Copy(ctx context.Context, source, destination string) error {
	return fs.CoreFS.Copy(ctx, source, destination)
}

func (fs *FileSystemAdapter) Move(ctx context.Context, source, destination string) error {
	return fs.CoreFS.Move(ctx, source, destination)
}

func (fs *FileSystemAdapter) Size(ctx context.Context, path string) (int64, error) {
	return fs.CoreFS.Size(ctx, path)
}

func (fs *FileSystemAdapter) LastModified(ctx context.Context, path string) (time.Time, error) {
	return fs.CoreFS.LastModified(ctx, path)
}

func (fs *FileSystemAdapter) MimeType(ctx context.Context, path string) (string, error) {
	return fs.CoreFS.MimeType(ctx, path)
}

func (fs *FileSystemAdapter) SetVisibility(ctx context.Context, path, visibility string) error {
	return fs.CoreFS.SetVisibility(ctx, path, visibility)
}

func (fs *FileSystemAdapter) Visibility(ctx context.Context, path string) (string, error) {
	return fs.CoreFS.Visibility(ctx, path)
}

func (fs *FileSystemAdapter) URL(ctx context.Context, path string) string {
	return fs.CoreFS.URL(ctx, path)
}

func (fs *FileSystemAdapter) TemporaryURL(ctx context.Context, path string, expiration time.Duration) (string, error) {
	return fs.CoreFS.TemporaryURL(ctx, path, expiration)
}

func (fs *FileSystemAdapter) Checksum(ctx context.Context, path string, algorithm string) (string, error) {
	return fs.CoreFS.Checksum(ctx, path, algorithm)
}

// ConvertToCoreWriteOptions 将storage.WriteOption转换为core.WriteOption
func ConvertToCoreWriteOptions(options []WriteOption) []core.WriteOption {
	if options == nil {
		return nil
	}

	coreOptions := make([]core.WriteOption, len(options))
	for i := range options {
		opt := options[i]
		coreOptions[i] = func(co *core.WriteOptions) {
			// 创建一个临时的storage.WriteOptions对象
			storageOpt := DefaultWriteOptions()
			opt(storageOpt)

			// 复制值到core.WriteOptions
			co.Visibility = storageOpt.Visibility
			co.Metadata = storageOpt.Metadata
			co.MimeType = storageOpt.MimeType
			co.Overwrite = storageOpt.Overwrite
			co.Permissions = os.FileMode(storageOpt.Permissions)
		}
	}
	return coreOptions
}

// ConvertToStorageWriteOptions 将core.WriteOption转换为storage.WriteOption
func ConvertToStorageWriteOptions(options []core.WriteOption) []WriteOption {
	if options == nil {
		return nil
	}

	storageOptions := make([]WriteOption, len(options))
	for i := range options {
		opt := options[i]
		storageOptions[i] = func(o *WriteOptions) {
			// 创建一个临时的core.WriteOptions对象
			coreOpt := core.DefaultWriteOptions()
			opt(coreOpt)

			// 复制值到storage.WriteOptions
			o.Visibility = coreOpt.Visibility
			o.Metadata = coreOpt.Metadata
			o.MimeType = coreOpt.MimeType
			o.Overwrite = coreOpt.Overwrite
			o.Permissions = os.FileMode(coreOpt.Permissions)
		}
	}
	return storageOptions
}

// ConvertToStorageFile 将core.File转换为storage.File
func ConvertToStorageFile(coreFile core.File) File {
	if coreFile == nil {
		return nil
	}
	return &FileAdapter{CoreFile: coreFile}
}

// ConvertToStorageFiles 将[]core.File转换为[]File
func ConvertToStorageFiles(coreFiles []core.File) []File {
	if coreFiles == nil {
		return nil
	}

	files := make([]File, len(coreFiles))
	for i, file := range coreFiles {
		files[i] = &FileAdapter{CoreFile: file}
	}
	return files
}

// ConvertToStorageFileSystem 将core.FileSystem转换为storage.FileSystem
func ConvertToStorageFileSystem(coreFS core.FileSystem) FileSystem {
	if coreFS == nil {
		return nil
	}
	return &FileSystemAdapter{CoreFS: coreFS}
}

// StorageToCoreFSAdapter 适配器，将storage.FileSystem转换为core.FileSystem
type StorageToCoreFSAdapter struct {
	StorageFS FileSystem
}

// 实现core.FileSystem接口
func (fs *StorageToCoreFSAdapter) Get(ctx context.Context, path string) (core.File, error) {
	file, err := fs.StorageFS.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	return &StorageToCoreFSFileAdapter{StorageFile: file}, nil
}

func (fs *StorageToCoreFSAdapter) Exists(ctx context.Context, path string) (bool, error) {
	return fs.StorageFS.Exists(ctx, path)
}

func (fs *StorageToCoreFSAdapter) Write(ctx context.Context, path string, content []byte, options ...core.WriteOption) error {
	// 将core.WriteOption转换为storage.WriteOption
	storageOptions := ConvertToStorageWriteOptions(options)
	return fs.StorageFS.Write(ctx, path, content, storageOptions...)
}

func (fs *StorageToCoreFSAdapter) WriteStream(ctx context.Context, path string, content io.Reader, options ...core.WriteOption) error {
	// 将core.WriteOption转换为storage.WriteOption
	storageOptions := ConvertToStorageWriteOptions(options)
	return fs.StorageFS.WriteStream(ctx, path, content, storageOptions...)
}

func (fs *StorageToCoreFSAdapter) Delete(ctx context.Context, path string) error {
	return fs.StorageFS.Delete(ctx, path)
}

func (fs *StorageToCoreFSAdapter) DeleteDirectory(ctx context.Context, path string) error {
	return fs.StorageFS.DeleteDirectory(ctx, path)
}

func (fs *StorageToCoreFSAdapter) CreateDirectory(ctx context.Context, path string, options ...core.WriteOption) error {
	// 将core.WriteOption转换为storage.WriteOption
	storageOptions := ConvertToStorageWriteOptions(options)
	return fs.StorageFS.CreateDirectory(ctx, path, storageOptions...)
}

func (fs *StorageToCoreFSAdapter) Files(ctx context.Context, directory string) ([]core.File, error) {
	files, err := fs.StorageFS.Files(ctx, directory)
	if err != nil {
		return nil, err
	}

	// 转换storage.File数组为core.File数组
	coreFiles := make([]core.File, len(files))
	for i, file := range files {
		coreFiles[i] = &StorageToCoreFSFileAdapter{StorageFile: file}
	}
	return coreFiles, nil
}

func (fs *StorageToCoreFSAdapter) AllFiles(ctx context.Context, directory string) ([]core.File, error) {
	files, err := fs.StorageFS.AllFiles(ctx, directory)
	if err != nil {
		return nil, err
	}

	// 转换storage.File数组为core.File数组
	coreFiles := make([]core.File, len(files))
	for i, file := range files {
		coreFiles[i] = &StorageToCoreFSFileAdapter{StorageFile: file}
	}
	return coreFiles, nil
}

func (fs *StorageToCoreFSAdapter) Directories(ctx context.Context, directory string) ([]string, error) {
	return fs.StorageFS.Directories(ctx, directory)
}

func (fs *StorageToCoreFSAdapter) AllDirectories(ctx context.Context, directory string) ([]string, error) {
	return fs.StorageFS.AllDirectories(ctx, directory)
}

func (fs *StorageToCoreFSAdapter) Copy(ctx context.Context, source, destination string) error {
	return fs.StorageFS.Copy(ctx, source, destination)
}

func (fs *StorageToCoreFSAdapter) Move(ctx context.Context, source, destination string) error {
	return fs.StorageFS.Move(ctx, source, destination)
}

func (fs *StorageToCoreFSAdapter) Size(ctx context.Context, path string) (int64, error) {
	return fs.StorageFS.Size(ctx, path)
}

func (fs *StorageToCoreFSAdapter) LastModified(ctx context.Context, path string) (time.Time, error) {
	return fs.StorageFS.LastModified(ctx, path)
}

func (fs *StorageToCoreFSAdapter) MimeType(ctx context.Context, path string) (string, error) {
	return fs.StorageFS.MimeType(ctx, path)
}

func (fs *StorageToCoreFSAdapter) SetVisibility(ctx context.Context, path, visibility string) error {
	return fs.StorageFS.SetVisibility(ctx, path, visibility)
}

func (fs *StorageToCoreFSAdapter) Visibility(ctx context.Context, path string) (string, error) {
	return fs.StorageFS.Visibility(ctx, path)
}

func (fs *StorageToCoreFSAdapter) URL(ctx context.Context, path string) string {
	return fs.StorageFS.URL(ctx, path)
}

func (fs *StorageToCoreFSAdapter) TemporaryURL(ctx context.Context, path string, expiration time.Duration) (string, error) {
	return fs.StorageFS.TemporaryURL(ctx, path, expiration)
}

func (fs *StorageToCoreFSAdapter) Checksum(ctx context.Context, path string, algorithm string) (string, error) {
	return fs.StorageFS.Checksum(ctx, path, algorithm)
}

// StorageToCoreFSFileAdapter 适配器，将storage.File转换为core.File
type StorageToCoreFSFileAdapter struct {
	StorageFile File
}

// 实现core.File接口
func (f *StorageToCoreFSFileAdapter) Path() string {
	return f.StorageFile.Path()
}

func (f *StorageToCoreFSFileAdapter) Name() string {
	return f.StorageFile.Name()
}

func (f *StorageToCoreFSFileAdapter) Extension() string {
	return f.StorageFile.Extension()
}

func (f *StorageToCoreFSFileAdapter) Size() int64 {
	return f.StorageFile.Size()
}

func (f *StorageToCoreFSFileAdapter) LastModified() time.Time {
	return f.StorageFile.LastModified()
}

func (f *StorageToCoreFSFileAdapter) IsDirectory() bool {
	return f.StorageFile.IsDirectory()
}

func (f *StorageToCoreFSFileAdapter) Read(ctx context.Context) ([]byte, error) {
	return f.StorageFile.Read(ctx)
}

func (f *StorageToCoreFSFileAdapter) ReadStream(ctx context.Context) (io.ReadCloser, error) {
	return f.StorageFile.ReadStream(ctx)
}

func (f *StorageToCoreFSFileAdapter) MimeType() string {
	return f.StorageFile.MimeType()
}

func (f *StorageToCoreFSFileAdapter) Visibility() string {
	return f.StorageFile.Visibility()
}

func (f *StorageToCoreFSFileAdapter) URL() string {
	return f.StorageFile.URL()
}

func (f *StorageToCoreFSFileAdapter) TemporaryURL(ctx context.Context, expiration time.Duration) (string, error) {
	return f.StorageFile.TemporaryURL(ctx, expiration)
}

func (f *StorageToCoreFSFileAdapter) Metadata() map[string]interface{} {
	return f.StorageFile.Metadata()
}

// ConvertToCoreFSFileSystem 将storage.FileSystem转换为core.FileSystem
func ConvertToCoreFSFileSystem(storageFS FileSystem) core.FileSystem {
	if storageFS == nil {
		return nil
	}
	return &StorageToCoreFSAdapter{StorageFS: storageFS}
}

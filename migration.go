package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MigrationOptions 迁移选项
type MigrationOptions struct {
	// 是否递归处理子目录
	Recursive bool

	// 文件匹配模式（例如 *.jpg）
	Pattern string

	// 是否保留目录结构
	PreserveStructure bool

	// 目标目录
	TargetDirectory string

	// 并发迁移的最大文件数
	MaxConcurrent int

	// 失败时是否继续
	ContinueOnError bool

	// 是否覆盖目标文件
	Overwrite bool

	// 处理进度回调
	ProgressCallback func(processed, total int, currentFile string)
}

// DefaultMigrationOptions 返回默认的迁移选项
func DefaultMigrationOptions() *MigrationOptions {
	return &MigrationOptions{
		Recursive:         true,
		Pattern:           "*",
		PreserveStructure: true,
		TargetDirectory:   "",
		MaxConcurrent:     4,
		ContinueOnError:   true,
		Overwrite:         false,
		ProgressCallback:  nil,
	}
}

// MigrationResult 迁移结果
type MigrationResult struct {
	// 成功迁移的文件数
	SuccessCount int

	// 失败的文件数
	FailedCount int

	// 总处理文件数
	TotalCount int

	// 失败的文件及错误
	Failures map[string]error

	// 开始时间
	StartTime time.Time

	// 结束时间
	EndTime time.Time

	// 耗时
	Duration time.Duration
}

// Migrator 文件迁移器
type Migrator struct {
	// 存储管理器
	storage *Manager
}

// NewMigrator 创建文件迁移器
func NewMigrator(manager *Manager) *Migrator {
	return &Migrator{
		storage: manager,
	}
}

// Migrate 在两个存储之间迁移文件
func (m *Migrator) Migrate(ctx context.Context, sourceDisk, targetDisk string, sourceDirectory string, options *MigrationOptions) (*MigrationResult, error) {
	if options == nil {
		options = DefaultMigrationOptions()
	}

	result := &MigrationResult{
		Failures:  make(map[string]error),
		StartTime: time.Now(),
	}

	// 获取源存储和目标存储
	source, err := m.storage.Disk(sourceDisk)
	if err != nil {
		return nil, fmt.Errorf("migration: 获取源存储失败: %w", err)
	}

	target, err := m.storage.Disk(targetDisk)
	if err != nil {
		return nil, fmt.Errorf("migration: 获取目标存储失败: %w", err)
	}

	// 收集要处理的文件
	var filesToProcess []string

	if options.Recursive {
		files, err := source.AllFiles(ctx, sourceDirectory)
		if err != nil {
			return nil, fmt.Errorf("migration: 获取所有文件失败: %w", err)
		}

		for _, file := range files {
			path := file.Path()
			// 检查文件是否匹配模式
			if matchPattern(filepath.Base(path), options.Pattern) {
				filesToProcess = append(filesToProcess, path)
			}
		}
	} else {
		files, err := source.Files(ctx, sourceDirectory)
		if err != nil {
			return nil, fmt.Errorf("migration: 获取目录文件失败: %w", err)
		}

		for _, file := range files {
			path := file.Path()
			// 检查文件是否匹配模式
			if matchPattern(filepath.Base(path), options.Pattern) {
				filesToProcess = append(filesToProcess, path)
			}
		}
	}

	// 设置进度信息
	totalFiles := len(filesToProcess)
	processedFiles := 0
	result.TotalCount = totalFiles

	// 限制并发数量
	concurrency := options.MaxConcurrent
	if concurrency <= 0 {
		concurrency = 1
	}

	// 使用信号量控制并发
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	// 处理每个文件
	for _, sourcePath := range filesToProcess {
		sourcePath := sourcePath // 创建副本避免闭包问题

		// 确定目标路径
		targetPath := sourcePath
		if options.PreserveStructure {
			// 保留目录结构但可能调整基目录
			relPath, err := getRelativePath(sourceDirectory, sourcePath)
			if err != nil && !options.ContinueOnError {
				result.Failures[sourcePath] = err
				result.FailedCount++
				continue
			}
			targetPath = filepath.Join(options.TargetDirectory, relPath)
		} else if options.TargetDirectory != "" {
			// 不保留目录结构，只使用文件名
			targetPath = filepath.Join(options.TargetDirectory, filepath.Base(sourcePath))
		}

		// 并发处理文件
		wg.Add(1)
		sem <- struct{}{} // 获取信号量

		go func() {
			defer wg.Done()
			defer func() { <-sem }() // 释放信号量

			// 检查目标文件是否存在
			exists, err := target.Exists(ctx, targetPath)
			if err != nil {
				if !options.ContinueOnError {
					result.Failures[sourcePath] = err
					result.FailedCount++
					return
				}
			}

			if exists && !options.Overwrite {
				result.Failures[sourcePath] = fmt.Errorf("目标文件已存在: %s", targetPath)
				result.FailedCount++
				return
			}

			// 读取源文件
			sourceFile, err := source.Get(ctx, sourcePath)
			if err != nil {
				result.Failures[sourcePath] = err
				result.FailedCount++
				return
			}

			// 获取文件元数据
			visibility := sourceFile.Visibility()
			mimeType := sourceFile.MimeType()
			metadata := sourceFile.Metadata()

			// 获取文件内容
			content, err := sourceFile.Read(ctx)
			if err != nil {
				result.Failures[sourcePath] = err
				result.FailedCount++
				return
			}

			// 创建目标目录（如果需要）
			targetDir := filepath.Dir(targetPath)
			if targetDir != "." && targetDir != "/" {
				err = target.CreateDirectory(ctx, targetDir)
				if err != nil && !isDirectoryExistsError(err) && !options.ContinueOnError {
					result.Failures[sourcePath] = err
					result.FailedCount++
					return
				}
			}

			// 写入目标文件
			writeOptions := []WriteOption{
				WithVisibility(visibility),
				WithMimeType(mimeType),
				WithMetadata(metadata),
				WithOverwrite(options.Overwrite),
			}

			err = target.Write(ctx, targetPath, content, writeOptions...)
			if err != nil {
				result.Failures[sourcePath] = err
				result.FailedCount++
				return
			}

			// 更新进度
			processedFiles++
			if options.ProgressCallback != nil {
				options.ProgressCallback(processedFiles, totalFiles, sourcePath)
			}

			result.SuccessCount++
		}()
	}

	// 等待所有任务完成
	wg.Wait()

	// 设置结果信息
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// Sync 同步两个存储之间的文件
func (m *Migrator) Sync(ctx context.Context, sourceDisk, targetDisk string, directory string, options *MigrationOptions) (*MigrationResult, error) {
	// Sync与Migrate类似，但只迁移目标中不存在或更新的文件
	if options == nil {
		options = DefaultMigrationOptions()
	}

	// 强制开启覆盖
	syncOptions := *options
	syncOptions.Overwrite = true

	result := &MigrationResult{
		Failures:  make(map[string]error),
		StartTime: time.Now(),
	}

	// 获取源存储和目标存储
	source, err := m.storage.Disk(sourceDisk)
	if err != nil {
		return nil, fmt.Errorf("sync: 获取源存储失败: %w", err)
	}

	target, err := m.storage.Disk(targetDisk)
	if err != nil {
		return nil, fmt.Errorf("sync: 获取目标存储失败: %w", err)
	}

	// 收集要处理的文件
	var filesToProcess []string
	var sourceFiles []File

	if options.Recursive {
		sourceFiles, err = source.AllFiles(ctx, directory)
		if err != nil {
			return nil, fmt.Errorf("sync: 获取所有文件失败: %w", err)
		}
	} else {
		sourceFiles, err = source.Files(ctx, directory)
		if err != nil {
			return nil, fmt.Errorf("sync: 获取目录文件失败: %w", err)
		}
	}

	// 过滤匹配的文件并检查是否需要同步
	for _, file := range sourceFiles {
		path := file.Path()
		// 检查文件是否匹配模式
		if matchPattern(filepath.Base(path), options.Pattern) {
			// 确定目标路径
			targetPath := path
			if options.PreserveStructure {
				// 保留目录结构但可能调整基目录
				relPath, err := getRelativePath(directory, path)
				if err != nil && !options.ContinueOnError {
					result.Failures[path] = err
					result.FailedCount++
					continue
				}
				targetPath = filepath.Join(options.TargetDirectory, relPath)
			} else if options.TargetDirectory != "" {
				// 不保留目录结构，只使用文件名
				targetPath = filepath.Join(options.TargetDirectory, filepath.Base(path))
			}

			// 检查文件是否需要更新
			needsUpdate, err := m.needsUpdate(ctx, target, path, targetPath, file)
			if err != nil {
				if !options.ContinueOnError {
					result.Failures[path] = err
					result.FailedCount++
					continue
				}
			}

			if needsUpdate {
				filesToProcess = append(filesToProcess, path)
			}
		}
	}

	// 执行迁移
	migrationResult, err := m.Migrate(ctx, sourceDisk, targetDisk, directory, &syncOptions)
	if err != nil {
		return nil, err
	}

	// 合并结果
	result.SuccessCount = migrationResult.SuccessCount
	result.FailedCount = migrationResult.FailedCount
	result.TotalCount = migrationResult.TotalCount
	result.Failures = migrationResult.Failures
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// needsUpdate 检查文件是否需要更新
func (m *Migrator) needsUpdate(ctx context.Context, target FileSystem, sourcePath, targetPath string, sourceFile File) (bool, error) {
	// 检查目标文件是否存在
	exists, err := target.Exists(ctx, targetPath)
	if err != nil {
		return false, err
	}

	// 如果目标文件不存在，需要更新
	if !exists {
		return true, nil
	}

	// 检查大小是否不同
	sourceSize := sourceFile.Size()
	targetSize, err := target.Size(ctx, targetPath)
	if err != nil {
		return false, err
	}
	if sourceSize != targetSize {
		return true, nil
	}

	// 检查修改时间是否更新
	sourceModTime := sourceFile.LastModified()
	targetModTime, err := target.LastModified(ctx, targetPath)
	if err != nil {
		return false, err
	}
	if sourceModTime.After(targetModTime) {
		return true, nil
	}

	// TODO: 可以添加更多检查，如内容校验和比较

	// 默认不需要更新
	return false, nil
}

// Copy 复制文件到不同目录
func (m *Migrator) Copy(ctx context.Context, disk, sourcePath, targetPath string) error {
	fs, err := m.storage.Disk(disk)
	if err != nil {
		return fmt.Errorf("copy: 获取存储失败: %w", err)
	}

	return fs.Copy(ctx, sourcePath, targetPath)
}

// BatchCopy 批量复制文件
func (m *Migrator) BatchCopy(ctx context.Context, disk string, sourceDirectory, targetDirectory string, options *MigrationOptions) (*MigrationResult, error) {
	if options == nil {
		options = DefaultMigrationOptions()
	}

	result := &MigrationResult{
		Failures:  make(map[string]error),
		StartTime: time.Now(),
	}

	// 获取存储
	fs, err := m.storage.Disk(disk)
	if err != nil {
		return nil, fmt.Errorf("batchCopy: 获取存储失败: %w", err)
	}

	// 收集要处理的文件
	var filesToProcess []string

	if options.Recursive {
		files, err := fs.AllFiles(ctx, sourceDirectory)
		if err != nil {
			return nil, fmt.Errorf("batchCopy: 获取所有文件失败: %w", err)
		}

		for _, file := range files {
			path := file.Path()
			// 检查文件是否匹配模式
			if matchPattern(filepath.Base(path), options.Pattern) {
				filesToProcess = append(filesToProcess, path)
			}
		}
	} else {
		files, err := fs.Files(ctx, sourceDirectory)
		if err != nil {
			return nil, fmt.Errorf("batchCopy: 获取目录文件失败: %w", err)
		}

		for _, file := range files {
			path := file.Path()
			// 检查文件是否匹配模式
			if matchPattern(filepath.Base(path), options.Pattern) {
				filesToProcess = append(filesToProcess, path)
			}
		}
	}

	// 设置进度信息
	totalFiles := len(filesToProcess)
	processedFiles := 0
	result.TotalCount = totalFiles

	// 限制并发数量
	concurrency := options.MaxConcurrent
	if concurrency <= 0 {
		concurrency = 1
	}

	// 使用信号量控制并发
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	// 处理每个文件
	for _, sourcePath := range filesToProcess {
		sourcePath := sourcePath // 创建副本避免闭包问题

		// 确定目标路径
		targetPath := sourcePath
		if options.PreserveStructure {
			// 保留目录结构但可能调整基目录
			relPath, err := getRelativePath(sourceDirectory, sourcePath)
			if err != nil && !options.ContinueOnError {
				result.Failures[sourcePath] = err
				result.FailedCount++
				continue
			}
			targetPath = filepath.Join(targetDirectory, relPath)
		} else {
			// 不保留目录结构，只使用文件名
			targetPath = filepath.Join(targetDirectory, filepath.Base(sourcePath))
		}

		// 并发处理文件
		wg.Add(1)
		sem <- struct{}{} // 获取信号量

		go func() {
			defer wg.Done()
			defer func() { <-sem }() // 释放信号量

			// 检查目标文件是否存在
			exists, err := fs.Exists(ctx, targetPath)
			if err != nil {
				if !options.ContinueOnError {
					result.Failures[sourcePath] = err
					result.FailedCount++
					return
				}
			}

			if exists && !options.Overwrite {
				result.Failures[sourcePath] = fmt.Errorf("目标文件已存在: %s", targetPath)
				result.FailedCount++
				return
			}

			// 创建目标目录（如果需要）
			targetDir := filepath.Dir(targetPath)
			if targetDir != "." && targetDir != "/" {
				err = fs.CreateDirectory(ctx, targetDir)
				if err != nil && !isDirectoryExistsError(err) && !options.ContinueOnError {
					result.Failures[sourcePath] = err
					result.FailedCount++
					return
				}
			}

			// 复制文件
			err = fs.Copy(ctx, sourcePath, targetPath)
			if err != nil {
				result.Failures[sourcePath] = err
				result.FailedCount++
				return
			}

			// 更新进度
			processedFiles++
			if options.ProgressCallback != nil {
				options.ProgressCallback(processedFiles, totalFiles, sourcePath)
			}

			result.SuccessCount++
		}()
	}

	// 等待所有任务完成
	wg.Wait()

	// 设置结果信息
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// Move 移动文件到不同目录
func (m *Migrator) Move(ctx context.Context, disk, sourcePath, targetPath string) error {
	fs, err := m.storage.Disk(disk)
	if err != nil {
		return fmt.Errorf("move: 获取存储失败: %w", err)
	}

	return fs.Move(ctx, sourcePath, targetPath)
}

// BatchMove 批量移动文件
func (m *Migrator) BatchMove(ctx context.Context, disk string, sourceDirectory, targetDirectory string, options *MigrationOptions) (*MigrationResult, error) {
	if options == nil {
		options = DefaultMigrationOptions()
	}

	result := &MigrationResult{
		Failures:  make(map[string]error),
		StartTime: time.Now(),
	}

	// 获取存储
	fs, err := m.storage.Disk(disk)
	if err != nil {
		return nil, fmt.Errorf("batchMove: 获取存储失败: %w", err)
	}

	// 收集要处理的文件
	var filesToProcess []string

	if options.Recursive {
		files, err := fs.AllFiles(ctx, sourceDirectory)
		if err != nil {
			return nil, fmt.Errorf("batchMove: 获取所有文件失败: %w", err)
		}

		for _, file := range files {
			path := file.Path()
			// 检查文件是否匹配模式
			if matchPattern(filepath.Base(path), options.Pattern) {
				filesToProcess = append(filesToProcess, path)
			}
		}
	} else {
		files, err := fs.Files(ctx, sourceDirectory)
		if err != nil {
			return nil, fmt.Errorf("batchMove: 获取目录文件失败: %w", err)
		}

		for _, file := range files {
			path := file.Path()
			// 检查文件是否匹配模式
			if matchPattern(filepath.Base(path), options.Pattern) {
				filesToProcess = append(filesToProcess, path)
			}
		}
	}

	// 设置进度信息
	totalFiles := len(filesToProcess)
	processedFiles := 0
	result.TotalCount = totalFiles

	// 限制并发数量
	concurrency := options.MaxConcurrent
	if concurrency <= 0 {
		concurrency = 1
	}

	// 使用信号量控制并发
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	// 处理每个文件
	for _, sourcePath := range filesToProcess {
		sourcePath := sourcePath // 创建副本避免闭包问题

		// 确定目标路径
		targetPath := sourcePath
		if options.PreserveStructure {
			// 保留目录结构但可能调整基目录
			relPath, err := getRelativePath(sourceDirectory, sourcePath)
			if err != nil && !options.ContinueOnError {
				result.Failures[sourcePath] = err
				result.FailedCount++
				continue
			}
			targetPath = filepath.Join(targetDirectory, relPath)
		} else {
			// 不保留目录结构，只使用文件名
			targetPath = filepath.Join(targetDirectory, filepath.Base(sourcePath))
		}

		// 并发处理文件
		wg.Add(1)
		sem <- struct{}{} // 获取信号量

		go func() {
			defer wg.Done()
			defer func() { <-sem }() // 释放信号量

			// 检查目标文件是否存在
			exists, err := fs.Exists(ctx, targetPath)
			if err != nil {
				if !options.ContinueOnError {
					result.Failures[sourcePath] = err
					result.FailedCount++
					return
				}
			}

			if exists && !options.Overwrite {
				result.Failures[sourcePath] = fmt.Errorf("目标文件已存在: %s", targetPath)
				result.FailedCount++
				return
			}

			// 创建目标目录（如果需要）
			targetDir := filepath.Dir(targetPath)
			if targetDir != "." && targetDir != "/" {
				err = fs.CreateDirectory(ctx, targetDir)
				if err != nil && !isDirectoryExistsError(err) && !options.ContinueOnError {
					result.Failures[sourcePath] = err
					result.FailedCount++
					return
				}
			}

			// 移动文件
			err = fs.Move(ctx, sourcePath, targetPath)
			if err != nil {
				result.Failures[sourcePath] = err
				result.FailedCount++
				return
			}

			// 更新进度
			processedFiles++
			if options.ProgressCallback != nil {
				options.ProgressCallback(processedFiles, totalFiles, sourcePath)
			}

			result.SuccessCount++
		}()
	}

	// 等待所有任务完成
	wg.Wait()

	// 设置结果信息
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// 辅助函数

// getRelativePath 获取相对路径
func getRelativePath(base, path string) (string, error) {
	// 标准化路径
	base = strings.TrimSuffix(base, "/") + "/"

	// 检查路径是否以基础路径开头
	if !strings.HasPrefix(path, base) {
		return "", fmt.Errorf("path '%s' is not under base '%s'", path, base)
	}

	// 返回相对路径
	return strings.TrimPrefix(path, base), nil
}

// matchPattern 检查文件名是否匹配模式
func matchPattern(filename, pattern string) bool {
	if pattern == "*" {
		return true
	}

	matched, err := filepath.Match(pattern, filename)
	if err != nil {
		return false
	}

	return matched
}

// isDirectoryExistsError 检查是否是目录已存在错误
func isDirectoryExistsError(err error) bool {
	return errors.Is(err, ErrDirectoryNotEmpty)
}

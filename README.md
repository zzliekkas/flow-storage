# flow-storage

flow/v2 核心框架的存储扩展

## 驱动

| 驱动 | 说明 |
|---|---|
| `local` | 本地文件系统 |
| `oss` | 阿里云 OSS（对象级 ACL） |
| `s3` | S3 兼容存储（对象级 ACL） |
| `cos` | 腾讯云 COS |
| `qiniu` | 七牛云 Kodo（空间级 ACL） |

## 七牛（qiniu）说明

- 配置：`access_key` / `secret_key` / `bucket` / `domain`
  - `domain` 需带协议，如 `https://cdn.example.com`，同时用于公开 URL 与私有签名 URL
- `URL`：`domain + key`
- `TemporaryURL`：七牛私有下载凭证（`?e=<deadline>&token=<token>`），公开空间同样可用（token 会被忽略）
- `Exists` / `Size` / `LastModified` / `MimeType` / `Checksum`：基于 `Stat`
  - `Checksum` 支持 `etag` / `qetag` / `hash`（返回七牛 hash）与 `md5`（对象有 md5 时）
- `Files` / `AllFiles` / `Directories` / `AllDirectories`：基于 `ListFiles` 分页实现
- `Copy` / `Move` / `DeleteDirectory`：基于七牛 API 实现（删除目录按前缀递归删除）
- `CreateDirectory`：七牛没有目录概念，空实现

### 可见性限制（重要）

七牛云只有**空间（Bucket）级 ACL**，没有对象级 `public` / `private`：

- `SetVisibility` 返回 `storage.ErrVisibilityNotSupported`
- `WriteStream` 的 `WithVisibility(...)` 不会改变对象权限
- 需要「永久公开下载」（例如 APK）时，请**单独建一个公开空间**，
  不要把存放私有媒体（封面/视频/头像）的空间整个设为公开
- `Visibility` 返回空间级可见性（读取一次后缓存）

### 集成测试

```bash
QINIU_TEST_AK=... QINIU_TEST_SK=... \
QINIU_TEST_BUCKET=... QINIU_TEST_DOMAIN=https://cdn.example.com \
go test ./cloud -run TestQiniuIntegration -v
```

未设置这些环境变量时集成测试会自动跳过。

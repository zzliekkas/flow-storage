package cloud

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/zzliekkas/flow-storage/v3"
)

// TestQiniuIntegration 真实七牛空间集成测试。
// 需要设置以下环境变量后运行：
//
//	QINIU_TEST_AK / QINIU_TEST_SK / QINIU_TEST_BUCKET / QINIU_TEST_DOMAIN
func TestQiniuIntegration(t *testing.T) {
	ak := os.Getenv("QINIU_TEST_AK")
	sk := os.Getenv("QINIU_TEST_SK")
	bucket := os.Getenv("QINIU_TEST_BUCKET")
	domain := os.Getenv("QINIU_TEST_DOMAIN")
	if ak == "" || sk == "" || bucket == "" || domain == "" {
		t.Skip("未设置 QINIU_TEST_AK/QINIU_TEST_SK/QINIU_TEST_BUCKET/QINIU_TEST_DOMAIN，跳过七牛集成测试")
	}

	fs, err := NewQiniu(QiniuConfig{AccessKey: ak, SecretKey: sk, Bucket: bucket, Domain: domain})
	if err != nil {
		t.Fatalf("NewQiniu: %v", err)
	}

	ctx := context.Background()
	key := fmt.Sprintf("flow-storage-tests/%d.txt", time.Now().UnixNano())
	content := []byte("hello qiniu " + time.Now().Format(time.RFC3339Nano))

	if err := fs.Write(ctx, key, content, storage.WithOverwrite(true)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	defer func() { _ = fs.Delete(ctx, key) }()

	exists, err := fs.Exists(ctx, key)
	if err != nil || !exists {
		t.Fatalf("Exists = %v, %v; want true, nil", exists, err)
	}

	file, err := fs.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if file.Size() != int64(len(content)) {
		t.Fatalf("Size = %d, want %d", file.Size(), len(content))
	}

	data, err := file.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("Read = %q, want %q", string(data), string(content))
	}

	signed, err := fs.TemporaryURL(ctx, key, 10*time.Minute)
	if err != nil {
		t.Fatalf("TemporaryURL: %v", err)
	}
	resp, err := http.Get(signed)
	if err != nil {
		t.Fatalf("GET signed URL: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signed URL status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read signed body: %v", err)
	}
	if string(body) != string(content) {
		t.Fatalf("signed body = %q, want %q", string(body), string(content))
	}

	if err := fs.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err = fs.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Fatal("object still exists after delete")
	}
}

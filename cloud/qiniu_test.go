package cloud

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	qiniuclient "github.com/qiniu/go-sdk/v7/client"
	"github.com/zzliekkas/flow-storage/v3"
)

func newTestQiniu(t *testing.T) *QiniuFileSystem {
	t.Helper()
	fs, err := NewQiniu(QiniuConfig{
		AccessKey: "test-ak",
		SecretKey: "test-sk",
		Bucket:    "test-bucket",
		Domain:    "https://cdn.example.com/",
	})
	if err != nil {
		t.Fatalf("NewQiniu: %v", err)
	}
	return fs
}

func TestNewQiniuValidation(t *testing.T) {
	cases := []struct {
		name string
		cfg  QiniuConfig
	}{
		{"missing access key", QiniuConfig{SecretKey: "sk", Bucket: "b"}},
		{"missing secret key", QiniuConfig{AccessKey: "ak", Bucket: "b"}},
		{"missing bucket", QiniuConfig{AccessKey: "ak", SecretKey: "sk"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewQiniu(tc.cfg); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestQiniuURL(t *testing.T) {
	fs := newTestQiniu(t)
	got := fs.URL(context.Background(), "uploads/2026/a.png")
	want := "https://cdn.example.com/uploads/2026/a.png"
	if got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
}

func TestQiniuURLWithoutScheme(t *testing.T) {
	fs, err := NewQiniu(QiniuConfig{
		AccessKey: "ak", SecretKey: "sk", Bucket: "b",
		Domain: "cdn.example.com",
	})
	if err != nil {
		t.Fatalf("NewQiniu: %v", err)
	}
	if got, want := fs.URL(context.Background(), "a.png"), "https://cdn.example.com/a.png"; got != want {
		t.Fatalf("URL() = %q, want %q (scheme should default to https)", got, want)
	}
}

func TestQiniuTemporaryURL(t *testing.T) {
	fs := newTestQiniu(t)
	got, err := fs.TemporaryURL(context.Background(), "uploads/a.png", time.Hour)
	if err != nil {
		t.Fatalf("TemporaryURL: %v", err)
	}
	if !strings.HasPrefix(got, "https://cdn.example.com/uploads/a.png?e=") {
		t.Fatalf("TemporaryURL() = %q, want signed URL under domain", got)
	}
	if !strings.Contains(got, "&token=") {
		t.Fatalf("TemporaryURL() = %q, want token query", got)
	}
	if idx := strings.Index(got, "e="); idx < 0 || !strings.Contains(got[idx:], "&") {
		t.Fatalf("TemporaryURL() = %q, want e and token params", got)
	}
}

func TestQiniuTemporaryURLWithoutDomain(t *testing.T) {
	fs, err := NewQiniu(QiniuConfig{AccessKey: "ak", SecretKey: "sk", Bucket: "b"})
	if err != nil {
		t.Fatalf("NewQiniu: %v", err)
	}
	if _, err := fs.TemporaryURL(context.Background(), "a", time.Hour); err == nil {
		t.Fatal("expected error when domain is empty")
	}
	if got := fs.URL(context.Background(), "a"); got != "" {
		t.Fatalf("URL() = %q, want empty when domain is empty", got)
	}
}

func TestQiniuSetVisibilityUnsupported(t *testing.T) {
	fs := newTestQiniu(t)
	err := fs.SetVisibility(context.Background(), "a.png", "public")
	if !errors.Is(err, storage.ErrVisibilityNotSupported) {
		t.Fatalf("SetVisibility error = %v, want ErrVisibilityNotSupported", err)
	}
}

func TestIsQiniuNotFound(t *testing.T) {
	if !isQiniuNotFound(&qiniuclient.ErrorInfo{Code: 612}) {
		t.Fatal("HTTP 612 should be treated as not found")
	}
	if !isQiniuNotFound(&qiniuclient.ErrorInfo{Errno: 612}) {
		t.Fatal("errno 612 should be treated as not found")
	}
	if !isQiniuNotFound(errors.New("no such file or directory")) {
		t.Fatal("no such file message should be treated as not found")
	}
	if isQiniuNotFound(&qiniuclient.ErrorInfo{Code: 403}) {
		t.Fatal("403 must not be treated as not found")
	}
	if isQiniuNotFound(nil) {
		t.Fatal("nil must not be treated as not found")
	}
}

func TestQiniuPutTime(t *testing.T) {
	if !qiniuPutTime(0).IsZero() {
		t.Fatal("zero putTime should map to zero time")
	}
	// 2020-09-13T12:26:40Z 对应的 100ns 单位时间戳
	const putTime = 16000000000000000
	got := qiniuPutTime(putTime)
	if got.IsZero() || got.Year() != 2020 {
		t.Fatalf("qiniuPutTime(%d) = %v, want a 2020 time", putTime, got)
	}
}

func TestQiniuDirPrefix(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"/uploads":     "uploads/",
		"uploads/":     "uploads/",
		"uploads/2026": "uploads/2026/",
		"\\uploads\\a": "uploads/a/",
	}
	for in, want := range cases {
		if got := qiniuDirPrefix(in); got != want {
			t.Fatalf("qiniuDirPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

package destination

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Destination S3 兼容对象存储目标
type S3Destination struct {
	endpoint  string
	region    string
	bucket    string
	prefix    string
	accessKey string
	secretKey string
	useSSL    bool
	pathStyle bool

	client *minio.Client
}

// S3Options S3 参数
type S3Options struct {
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	PathStyle bool
}

// NewS3Destination 初始化 S3 目标
func NewS3Destination(opts S3Options) (*S3Destination, error) {
	ep := opts.Endpoint
	// 处理 endpoint 去除 http:// 或 https://
	useSSL := opts.UseSSL
	if strings.HasPrefix(ep, "https://") {
		ep = strings.TrimPrefix(ep, "https://")
		useSSL = true
	} else if strings.HasPrefix(ep, "http://") {
		ep = strings.TrimPrefix(ep, "http://")
		useSSL = false
	}
	ep = strings.TrimRight(ep, "/")

	d := &S3Destination{
		endpoint:  ep,
		region:    opts.Region,
		bucket:    opts.Bucket,
		prefix:    strings.Trim(opts.Prefix, "/"),
		accessKey: opts.AccessKey,
		secretKey: opts.SecretKey,
		useSSL:    useSSL,
		pathStyle: opts.PathStyle,
	}

	cli, err := minio.New(ep, &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, ""),
		Secure: useSSL,
		Region: opts.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 S3 客户端失败: %w", err)
	}
	d.client = cli
	return d, nil
}

func (s *S3Destination) objectKey(backupID string) string {
	name := backupID
	if !strings.HasSuffix(name, ".backup") {
		name = name + ".backup"
	}
	if s.prefix != "" {
		return s.prefix + "/" + name
	}
	return name
}

func (s *S3Destination) Validate(_ context.Context) error {
	if s.endpoint == "" {
		return errors.New("S3 Endpoint 不能为空")
	}
	if s.bucket == "" {
		return errors.New("S3 Bucket 不能为空")
	}
	if s.accessKey == "" || s.secretKey == "" {
		return errors.New("S3 访问密钥 AccessKey/SecretKey 不能为空")
	}
	return nil
}

func (s *S3Destination) TestConnection(ctx context.Context) error {
	if err := s.Validate(ctx); err != nil {
		return err
	}
	// 检查存储桶存在性
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("检测 S3 存储桶失败: %w", err)
	}
	if !exists {
		return fmt.Errorf("S3 存储桶 %q 不存在", s.bucket)
	}

	// 尝试写入探测文件并立即删除
	testKey := fmt.Sprintf(".test_probe_%d.tmp", time.Now().UnixNano())
	if s.prefix != "" {
		testKey = s.prefix + "/" + testKey
	}
	testContent := []byte("aie-backup-connection-test")
	_, err = s.client.PutObject(ctx, s.bucket, testKey, bytes.NewReader(testContent), int64(len(testContent)), minio.PutObjectOptions{
		ContentType: "text/plain",
	})
	if err != nil {
		return fmt.Errorf("S3 测试写入对象失败: %w", err)
	}

	_ = s.client.RemoveObject(ctx, s.bucket, testKey, minio.RemoveObjectOptions{})
	return nil
}

func (s *S3Destination) Put(ctx context.Context, backupID string, reader io.Reader, size int64) (string, error) {
	key := s.objectKey(backupID)
	// 上传临时对象
	tmpKey := key + ".uploading"

	_, err := s.client.PutObject(ctx, s.bucket, tmpKey, reader, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return "", fmt.Errorf("上传备份到 S3 失败: %w", err)
	}

	// S3 重命名通过 CopyObject + RemoveObject 实现
	src := minio.CopySrcOptions{
		Bucket: s.bucket,
		Object: tmpKey,
	}
	dst := minio.CopyDestOptions{
		Bucket: s.bucket,
		Object: key,
	}
	if _, err := s.client.CopyObject(ctx, dst, src); err != nil {
		_ = s.client.RemoveObject(ctx, s.bucket, tmpKey, minio.RemoveObjectOptions{})
		return "", fmt.Errorf("S3 原子重命名对象失败: %w", err)
	}

	_ = s.client.RemoveObject(ctx, s.bucket, tmpKey, minio.RemoveObjectOptions{})
	return fmt.Sprintf("s3://%s/%s", s.bucket, key), nil
}

func (s *S3Destination) Exists(ctx context.Context, backupID string) (bool, error) {
	key := s.objectKey(backupID)
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" || strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *S3Destination) Get(ctx context.Context, backupID string) (io.ReadCloser, error) {
	key := s.objectKey(backupID)
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// 验证对象是否存在
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, ErrDestNotFound
	}
	return obj, nil
}

func (s *S3Destination) Delete(ctx context.Context, backupID string) error {
	key := s.objectKey(backupID)
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *S3Destination) List(ctx context.Context) ([]BackupObject, error) {
	prefix := s.prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}
	var out []BackupObject
	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}
	for obj := range s.client.ListObjects(ctx, s.bucket, opts) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		if !strings.HasSuffix(obj.Key, ".backup") {
			continue
		}
		bID := strings.TrimSuffix(strings.TrimPrefix(obj.Key, prefix), ".backup")
		out = append(out, BackupObject{
			BackupID:   bID,
			RemotePath: fmt.Sprintf("s3://%s/%s", s.bucket, obj.Key),
			Size:       obj.Size,
			ModTime:    obj.LastModified,
		})
	}
	return out, nil
}

func (s *S3Destination) GetUsage(ctx context.Context) (*StorageUsage, error) {
	list, err := s.List(ctx)
	if err != nil {
		return &StorageUsage{}, nil
	}
	var used uint64
	for _, it := range list {
		used += uint64(it.Size)
	}
	return &StorageUsage{UsedBytes: used}, nil
}

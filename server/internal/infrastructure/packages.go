package infrastructure

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const MaxPackageSize int64 = 512 << 20
const maxBinarySize int64 = 256 << 20

func validateRelease(r Release) error {
	if !safeName.MatchString(r.Version) || r.OS != "linux" && r.OS != "windows" && r.OS != "darwin" || r.Arch != "amd64" && r.Arch != "arm64" || r.Format != "binary" && r.Format != "tar.gz" && r.Format != "zip" {
		return fmt.Errorf("%w: 版本、平台、架构或包格式无效", ErrInvalid)
	}
	if r.Entry != "" && (path.IsAbs(r.Entry) || path.Clean(r.Entry) != r.Entry || strings.Contains(r.Entry, "\\") || r.Entry == ".." || strings.HasPrefix(r.Entry, "../")) {
		return ErrInvalid
	}
	if r.SHA256 != "" {
		b, err := hex.DecodeString(r.SHA256)
		if err != nil || len(b) != 32 {
			return fmt.Errorf("%w: SHA256 必须为 64 位十六进制", ErrInvalid)
		}
	}
	return nil
}

// AddRelease 上传内容以 ID 命名，下载文件名只用于展示，不参与磁盘路径。
func (s *Service) AddRelease(ctx context.Context, r Release, src io.Reader) (Release, error) {
	if _, err := load[Software](ctx, s.Store, "software", r.SoftwareID); err != nil {
		return r, err
	}
	if err := validateRelease(r); err != nil {
		return r, err
	}
	if src == nil {
		if err := validateURL(r.URL, true); err != nil {
			return r, err
		}
		if r.SHA256 == "" {
			return r, fmt.Errorf("在线版本须提供 SHA256 以校验安装包")
		}
	}
	r.ID = newID()
	r.CreatedAt = time.Now().UTC()
	r.Cached = false
	r.Size = 0
	if src != nil {
		if err := s.cache(ctx, &r, src); err != nil {
			return r, err
		}
		defer func() {
			if !r.Cached {
				_ = os.Remove(s.artifactPath(r.ID))
			}
		}()
	}
	if err := save(ctx, s.Store, "release", r.ID, r, true); err != nil {
		_ = os.Remove(s.artifactPath(r.ID))
		return r, err
	}
	return r, nil
}

func (s *Service) cache(ctx context.Context, r *Release, src io.Reader) error {
	if err := os.MkdirAll(s.Root, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Root, "package-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(src, MaxPackageSize+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if n == 0 || n > MaxPackageSize {
		return fmt.Errorf("软件包为空或超过 512 MiB")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if r.SHA256 != "" && !strings.EqualFold(r.SHA256, digest) {
		return fmt.Errorf("软件包 SHA256 不匹配")
	}
	r.SHA256 = digest
	r.Size = n
	if err := os.Rename(tmp, s.artifactPath(r.ID)); err != nil {
		existing, openErr := os.Open(s.artifactPath(r.ID))
		if openErr != nil {
			return err
		}
		verifyErr := verifyFile(existing, digest)
		_ = existing.Close()
		if verifyErr != nil {
			return err
		}
	}
	r.Cached = true
	return nil
}

func (s *Service) download(ctx context.Context, raw, secretID, actor string) (*http.Response, error) {
	if err := validateURL(raw, true); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, application/octet-stream")
	req.Header.Set("User-Agent", "AI-Employee-Platform")
	if secretID != "" {
		token, err := s.resolve(ctx, secretID, actor, "software.download")
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(token, "\r\n") {
			return nil, ErrInvalid
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 5 * time.Minute, Transport: s.downloadTransport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != "https" {
			return fmt.Errorf("下载重定向无效")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("更新源连接失败")
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("更新源返回 HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (s *Service) CachedPackage(ctx context.Context, id, actor string) (Release, *os.File, error) {
	lock, _ := s.packageLocks.LoadOrStore(id, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	r, err := load[Release](ctx, s.Store, "release", id)
	if err != nil {
		return r, nil, err
	}
	if !r.Cached {
		sw, err := load[Software](ctx, s.Store, "software", r.SoftwareID)
		if err != nil {
			return r, nil, err
		}
		resp, err := s.download(ctx, r.URL, packageSecret(sw, r.URL), actor)
		if err != nil {
			return r, nil, err
		}
		defer resp.Body.Close()
		if err := s.cache(ctx, &r, resp.Body); err != nil {
			return r, nil, err
		}
		if err := save(ctx, s.Store, "release", r.ID, r, false); err != nil {
			return r, nil, err
		}
	}
	f, err := os.Open(s.artifactPath(r.ID))
	if err != nil {
		return r, nil, fmt.Errorf("软件包文件缺失，请恢复软件包存储目录")
	}
	if err := verifyFile(f, r.SHA256); err != nil {
		f.Close()
		return r, nil, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		f.Close()
		return r, nil, err
	}
	return r, f, nil
}

func verifyFile(f *os.File, expected string) error {
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, MaxPackageSize+1)); err != nil {
		return err
	}
	if expected == "" || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), expected) {
		return fmt.Errorf("缓存安装包校验失败，请恢复可信的软件包")
	}
	return nil
}

// binaryReader 只提取匹配的单个普通文件，不将归档路径或软链接解压到磁盘。
func binaryReader(f *os.File, r Release, binary string) (io.Reader, error) {
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	match := func(name string) bool {
		clean := strings.TrimPrefix(name, "./")
		if r.Entry != "" {
			return clean == r.Entry
		}
		base := path.Base(clean)
		return base == binary || strings.HasPrefix(base, binary+"-")
	}
	switch r.Format {
	case "binary":
		if r.Size > maxBinarySize {
			return nil, fmt.Errorf("程序文件超过 256 MiB")
		}
		return io.LimitReader(f, maxBinarySize+1), nil
	case "tar.gz":
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		tr := tar.NewReader(io.LimitReader(gz, 2<<30))
		for count := 0; count < 10000; count++ {
			h, err := tr.Next()
			if err != nil {
				gz.Close()
				return nil, fmt.Errorf("归档中找不到程序，请填写程序在包内的路径")
			}
			if match(h.Name) {
				if h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > maxBinarySize {
					gz.Close()
					return nil, fmt.Errorf("包内程序必须是有效的普通文件")
				}
				return io.LimitReader(tr, h.Size), nil
			}
		}
	case "zip":
		stat, err := f.Stat()
		if err != nil {
			return nil, err
		}
		zr, err := zip.NewReader(f, stat.Size())
		if err != nil {
			return nil, err
		}
		for _, entry := range zr.File {
			if match(entry.Name) {
				if !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > uint64(maxBinarySize) {
					return nil, ErrInvalid
				}
				return entry.Open()
			}
		}
	}
	return nil, fmt.Errorf("归档中找不到程序，请填写程序在包内的路径")
}

// CheckUpdates 支持带版本清单的 HTTPS 源和 GitHub Release；空源返回本地版本。
func (s *Service) CheckUpdates(ctx context.Context, id, actor string) ([]Release, error) {
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	sw, err := load[Software](ctx, s.Store, "software", id)
	if err != nil {
		return nil, err
	}
	if sw.SourceURL == "" {
		return s.softwareReleases(ctx, id)
	}
	result, err := s.checkSource(ctx, sw, actor)
	sw.LastChecked = time.Now().UTC()
	sw.LastError = ""
	if err != nil {
		sw.LastError = err.Error()
	}
	_, saveErr := mutate[Software](ctx, s.Store, "software", sw.ID, func(current *Software) error {
		if current.SourceURL == sw.SourceURL && current.SourceSecretID == sw.SourceSecretID {
			current.LastChecked = sw.LastChecked
			current.LastError = sw.LastError
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if saveErr != nil {
		return nil, saveErr
	}
	return result, nil
}
func (s *Service) softwareReleases(ctx context.Context, id string) ([]Release, error) {
	all, err := s.Releases(ctx)
	if err != nil {
		return nil, err
	}
	out := []Release{}
	for _, r := range all {
		if r.SoftwareID == id {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return newer(out[i].Version, out[j].Version) })
	return out, nil
}

func (s *Service) checkSource(ctx context.Context, sw Software, actor string) ([]Release, error) {
	resp, err := s.download(ctx, sw.SourceURL, sw.SourceSecretID, actor)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var candidates []Release
	if sw.SourceType == "manifest" {
		var m struct {
			Releases []Release `json:"releases"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&m); err != nil {
			return nil, fmt.Errorf("版本清单 JSON 无效")
		}
		candidates = m.Releases
	} else if sw.SourceType == "github" {
		var g struct {
			Tag        string `json:"tag_name"`
			Draft      bool   `json:"draft"`
			Prerelease bool   `json:"prerelease"`
			Assets     []struct {
				Name   string `json:"name"`
				URL    string `json:"browser_download_url"`
				Digest string `json:"digest"`
			} `json:"assets"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&g); err != nil {
			return nil, err
		}
		if g.Draft || g.Prerelease {
			return nil, fmt.Errorf("更新源返回草稿或预发布版本")
		}
		arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[sw.SourceArch]
		pattern := strings.ReplaceAll(sw.AssetPattern, "{arch}", arch)
		for _, a := range g.Assets {
			if a.Name != pattern {
				continue
			}
			format := "binary"
			if strings.HasSuffix(a.Name, ".tar.gz") {
				format = "tar.gz"
			} else if strings.HasSuffix(a.Name, ".zip") {
				format = "zip"
			}
			candidates = append(candidates, Release{SoftwareID: sw.ID, Version: strings.TrimPrefix(strings.TrimPrefix(g.Tag, "rust-v"), "v"), OS: sw.SourceOS, Arch: sw.SourceArch, URL: a.URL, SHA256: strings.TrimPrefix(a.Digest, "sha256:"), Format: format})
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("更新源没有匹配平台的安装包")
		}
	} else {
		return nil, ErrInvalid
	}
	existing, err := s.softwareReleases(ctx, sw.ID)
	if err != nil {
		return nil, err
	}
	for _, r := range candidates {
		r.SoftwareID = sw.ID
		found := false
		for _, old := range existing {
			if old.Version == r.Version && old.OS == r.OS && old.Arch == r.Arch {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if err := validateRelease(r); err != nil {
			return nil, err
		}
		if err := validateURL(r.URL, true); err != nil {
			return nil, err
		}
		// 部分 GitHub 发布未返回 digest，先缓存并计算哈希，再发布为不可变版本。
		if r.SHA256 == "" {
			res, err := s.download(ctx, r.URL, packageSecret(sw, r.URL), actor)
			if err != nil {
				return nil, err
			}
			r, err = s.AddRelease(ctx, r, res.Body)
			res.Body.Close()
			if err != nil {
				return nil, err
			}
		} else {
			r, err = s.AddRelease(ctx, r, nil)
			if err != nil && err != ErrConflict {
				return nil, err
			}
		}
	}
	return s.softwareReleases(ctx, sw.ID)
}

// newer 对数字分段比较，避免把 1.10 排在 1.9 之前；稳定版优先于同号预发布。
func newer(a, b string) bool {
	parse := func(v string) ([]uint64, []string, bool) {
		v = strings.SplitN(strings.TrimPrefix(v, "v"), "+", 2)[0]
		parts := strings.SplitN(v, "-", 2)
		core := []uint64{}
		for _, p := range strings.Split(parts[0], ".") {
			n, err := strconv.ParseUint(p, 10, 64)
			if err != nil {
				return nil, nil, false
			}
			core = append(core, n)
		}
		var pre []string
		if len(parts) == 2 {
			pre = strings.Split(parts[1], ".")
		}
		return core, pre, true
	}
	aa, ap, aok := parse(a)
	bb, bp, bok := parse(b)
	if !aok || !bok {
		return a > b
	}
	for i := 0; i < len(aa) || i < len(bb); i++ {
		var x, y uint64
		if i < len(aa) {
			x = aa[i]
		}
		if i < len(bb) {
			y = bb[i]
		}
		if x != y {
			return x > y
		}
	}
	if len(ap) == 0 || len(bp) == 0 {
		return len(ap) == 0 && len(bp) > 0
	}
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] == bp[i] {
			continue
		}
		x, xe := strconv.ParseUint(ap[i], 10, 64)
		y, ye := strconv.ParseUint(bp[i], 10, 64)
		if xe == nil && ye == nil {
			return x > y
		}
		if xe == nil {
			return false
		}
		if ye == nil {
			return true
		}
		return ap[i] > bp[i]
	}
	return len(ap) > len(bp)
}

func filename(r Release) string {
	ext := map[string]string{"binary": "", "tar.gz": ".tar.gz", "zip": ".zip"}[r.Format]
	return r.SoftwareID + "-" + r.Version + "-" + r.OS + "-" + r.Arch + ext
}
func (s *Service) PackageName(r Release) string { return filepath.Base(filename(r)) }

// 同源令牌只能用于该源下载；URL 中禁止嵌入账号密码。
func sameOrigin(a, b string) bool {
	ua, e1 := url.Parse(a)
	ub, e2 := url.Parse(b)
	return e1 == nil && e2 == nil && ua.Scheme == ub.Scheme && ua.Host == ub.Host
}

func packageSecret(sw Software, raw string) string {
	if sameOrigin(sw.SourceURL, raw) {
		return sw.SourceSecretID
	}
	return ""
}

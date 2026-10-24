package infrastructure

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PVEClient 使用 API Token；任何调用均验证 TLS，不把 Token 返回前端。
type PVEClient struct {
	Cluster Cluster
	Token   string
	HTTP    *http.Client
}

func newPVE(c Cluster, token string) (*PVEClient, error) {
	if err := validateURL(c.URL, true); err != nil {
		return nil, err
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if c.CAPEM != "" && !roots.AppendCertsFromPEM([]byte(c.CAPEM)) {
		return nil, fmt.Errorf("PVE CA 证书格式无效")
	}
	token = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "PVEAPIToken="))
	if !strings.Contains(token, "!") || !strings.Contains(token, "=") || strings.ContainsAny(token, "\r\n") {
		return nil, fmt.Errorf("PVE Token 应为 user@realm!tokenid=secret")
	}
	return &PVEClient{Cluster: c, Token: token, HTTP: &http.Client{Timeout: 45 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 2, MaxIdleConnsPerHost: 2, TLSHandshakeTimeout: 15 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("PVE API 不允许重定向") }}}, nil
}

func validateURL(raw string, https bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" {
		return fmt.Errorf("%w: 地址必须是无凭证、查询参数的完整 URL", ErrInvalid)
	}
	if u.Scheme != "https" && (https || u.Scheme != "http") {
		return fmt.Errorf("%w: 地址必须使用 %s", ErrInvalid, map[bool]string{true: "HTTPS", false: "HTTP 或 HTTPS"}[https])
	}
	return nil
}

func (p *PVEClient) call(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.Cluster.URL, "/")+"/api2/json"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+p.Token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := p.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("PVE 连接失败，请检查地址、网络和 CA 证书: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("PVE API 返回 HTTP %d，请检查权限和参数", res.StatusCode)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("PVE 响应格式无效")
	}
	if out != nil {
		return json.Unmarshal(envelope.Data, out)
	}
	return nil
}

func (p *PVEClient) inventory(ctx context.Context) ([]Node, []Host, error) {
	nodes := []Node{}
	if err := p.call(ctx, "GET", "/nodes", nil, &nodes); err != nil {
		return nil, nil, err
	}
	var resources []struct {
		VMID     int     `json:"vmid"`
		Node     string  `json:"node"`
		Name     string  `json:"name"`
		Type     string  `json:"type"`
		Status   string  `json:"status"`
		Template int     `json:"template"`
		CPU      float64 `json:"cpu"`
		MaxCPU   int     `json:"maxcpu"`
		Mem      int64   `json:"mem"`
		MaxMem   int64   `json:"maxmem"`
		Disk     int64   `json:"disk"`
		MaxDisk  int64   `json:"maxdisk"`
	}
	if err := p.call(ctx, "GET", "/cluster/resources?type=vm", nil, &resources); err != nil {
		return nil, nil, err
	}
	hosts := []Host{}
	for _, v := range resources {
		if v.Type != "qemu" {
			continue
		}
		hosts = append(hosts, Host{ID: vmHostID(p.Cluster.ID, v.VMID), ClusterID: p.Cluster.ID, VMID: v.VMID, Node: v.Node, Name: v.Name, Status: v.Status, Template: v.Template == 1, CPU: v.CPU, Cores: v.MaxCPU, Memory: v.Mem, MaxMemory: v.MaxMem, Disk: v.Disk, MaxDisk: v.MaxDisk, LastSync: time.Now().UTC()})
	}
	return nodes, hosts, nil
}

func vmHostID(cluster string, vmid int) string { return cluster + "-" + strconv.Itoa(vmid) }
func vmPath(h Host) string {
	return "/nodes/" + url.PathEscape(h.Node) + "/qemu/" + strconv.Itoa(h.VMID)
}

func (p *PVEClient) wait(ctx context.Context, node, upid string) error {
	if upid == "" {
		return nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var st struct {
			Status string `json:"status"`
			Exit   string `json:"exitstatus"`
		}
		if err := p.call(ctx, "GET", "/nodes/"+url.PathEscape(node)+"/tasks/"+url.PathEscape(upid)+"/status", nil, &st); err != nil {
			return err
		}
		if st.Status == "stopped" {
			if st.Exit != "OK" {
				return fmt.Errorf("PVE 任务未成功结束，请在 PVE 查看任务日志")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *PVEClient) power(ctx context.Context, h Host, action string) error {
	if action != "start" && action != "shutdown" && action != "reboot" {
		return ErrInvalid
	}
	var upid string
	if err := p.call(ctx, "POST", vmPath(h)+"/status/"+action, url.Values{}, &upid); err != nil {
		return err
	}
	return p.wait(ctx, h.Node, upid)
}

func (p *PVEClient) clone(ctx context.Context, v VMCreate, step func(string) error) error {
	// 已存在的目标不自动接管，避免重试时覆盖同 VMID 的其他机器。
	var existing any
	if err := p.call(ctx, "GET", "/nodes/"+url.PathEscape(v.Node)+"/qemu/"+strconv.Itoa(v.VMID)+"/status/current", nil, &existing); err == nil {
		return fmt.Errorf("目标 VMID 已存在，请核对 PVE 后使用已有虚拟机")
	}
	if err := step("克隆系统模板"); err != nil {
		return err
	}
	form := url.Values{"newid": {strconv.Itoa(v.VMID)}, "name": {v.Name}, "target": {v.Node}, "full": {"1"}}
	if v.Storage != "" {
		form.Set("storage", v.Storage)
	}
	var upid string
	if err := p.call(ctx, "POST", "/nodes/"+url.PathEscape(v.TemplateNode)+"/qemu/"+strconv.Itoa(v.TemplateID)+"/clone", form, &upid); err != nil {
		return err
	}
	if err := p.wait(ctx, v.TemplateNode, upid); err != nil {
		return err
	}
	if err := step("配置 Cloud-init 与资源"); err != nil {
		return err
	}
	h := Host{Node: v.Node, VMID: v.VMID}
	config := url.Values{"cores": {strconv.Itoa(v.Cores)}, "memory": {strconv.Itoa(v.MemoryMB)}, "ciuser": {v.SSHUser}, "ipconfig0": {v.IPConfig}, "agent": {"enabled=1"}}
	if v.PublicKey != "" {
		config.Set("sshkeys", url.QueryEscape(v.PublicKey))
	}
	if v.Bridge != "" {
		config.Set("net0", "virtio,bridge="+v.Bridge)
	}
	if err := p.call(ctx, "PUT", vmPath(h)+"/config", config, nil); err != nil {
		return err
	}
	if v.DiskGB > 0 {
		if err := step("扩展虚拟磁盘"); err != nil {
			return err
		}
		if err := p.call(ctx, "PUT", vmPath(h)+"/resize", url.Values{"disk": {v.DiskDevice}, "size": {strconv.Itoa(v.DiskGB) + "G"}}, nil); err != nil {
			return err
		}
	}
	if err := step("启动虚拟机"); err != nil {
		return err
	}
	return p.power(ctx, h, "start")
}

func (p *PVEClient) guestAddress(ctx context.Context, h Host) (string, error) {
	var result struct {
		Result []struct {
			Name      string `json:"name"`
			Addresses []struct {
				Address string `json:"ip-address"`
				Type    string `json:"ip-address-type"`
			} `json:"ip-addresses"`
		} `json:"result"`
	}
	if err := p.call(ctx, "GET", vmPath(h)+"/agent/network-get-interfaces", nil, &result); err != nil {
		return "", err
	}
	for _, nic := range result.Result {
		if nic.Name == "lo" {
			continue
		}
		for _, a := range nic.Addresses {
			if a.Type == "ipv4" && !strings.HasPrefix(a.Address, "127.") && !strings.HasPrefix(a.Address, "169.254.") {
				return a.Address, nil
			}
		}
	}
	return "", fmt.Errorf("Guest Agent 尚未返回可用地址")
}

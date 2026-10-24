package infrastructure

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPVEInventoryUsesTLSAndToken(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "PVEAPIToken=user@pve!test=value" {
			t.Error("missing token")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api2/json/nodes":
			_, _ = w.Write([]byte(`{"data":[{"node":"node1","status":"online","cpu":0.1}]}`))
		case "/api2/json/cluster/resources":
			_, _ = w.Write([]byte(`{"data":[{"type":"qemu","vmid":100,"node":"node1","name":"vm","status":"running","maxcpu":2,"maxmem":4294967296},{"type":"lxc","vmid":101}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	client, err := newPVE(Cluster{ID: "cluster", URL: server.URL, CAPEM: ca}, "user@pve!test=value")
	if err != nil {
		t.Fatal(err)
	}
	nodes, hosts, err := client.inventory(context.Background())
	if err != nil || len(nodes) != 1 || len(hosts) != 1 || hosts[0].ID != "cluster-100" || hosts[0].Cores != 2 {
		t.Fatal(nodes, hosts, err)
	}
	untrusted, _ := newPVE(Cluster{URL: server.URL}, "user@pve!test=value")
	if _, _, err := untrusted.inventory(context.Background()); err == nil {
		t.Fatal("self signed certificate accepted without CA")
	}
}

func TestCloneConfiguresCloudInitAndWaitsForPVE(t *testing.T) {
	calls := []string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/status/current"):
			w.WriteHeader(404)
			return
		case strings.HasSuffix(r.URL.Path, "/clone"):
			_ = r.ParseForm()
			if r.Form.Get("newid") != "200" || r.Form.Get("full") != "1" {
				t.Error(r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:task"})
		case strings.Contains(r.URL.Path, "/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"status": "stopped", "exitstatus": "OK"}})
		case strings.HasSuffix(r.URL.Path, "/config"):
			_ = r.ParseForm()
			if r.Form.Get("cores") != "4" || r.Form.Get("ciuser") != "ubuntu" || r.Form.Get("ipconfig0") != "ip=dhcp" {
				t.Error(r.Form)
			}
			_, _ = w.Write([]byte(`{"data":null}`))
		case strings.HasSuffix(r.URL.Path, "/resize"):
			_ = r.ParseForm()
			if r.Form.Get("disk") != "scsi0" || r.Form.Get("size") != "40G" {
				t.Error(r.Form)
			}
			_, _ = w.Write([]byte(`{"data":null}`))
		case strings.HasSuffix(r.URL.Path, "/status/start"):
			_, _ = w.Write([]byte(`{"data":"UPID:start"}`))
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer server.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	p, _ := newPVE(Cluster{URL: server.URL, CAPEM: ca}, "user@pve!test=value")
	err := p.clone(context.Background(), VMCreate{TemplateNode: "n1", TemplateID: 100, VMID: 200, Node: "n1", Name: "new-vm", Cores: 4, MemoryMB: 4096, SSHUser: "ubuntu", IPConfig: "ip=dhcp", DiskGB: 40, DiskDevice: "scsi0"}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 7 {
		t.Fatal(calls)
	}
}

func TestSyncPreservesCredentialsAndMigrationIdentity(t *testing.T) {
	s := testService(t)
	h := testHost(t, s)
	h.ID = "cluster-100"
	h.ClusterID = "cluster"
	h.VMID = 100
	h.Node = "old-node"
	h.WorkstationID = "WS-1"
	h.ProvisionedBy = "clone-operation"
	_ = save(context.Background(), s.Store, "host", h.ID, h, false)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/nodes") {
			_, _ = w.Write([]byte(`{"data":[{"node":"new-node","status":"online"}]}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"type":"qemu","vmid":100,"node":"new-node","name":"vm","status":"running"}]}`))
		}
	}))
	defer server.Close()
	cred, err := s.SaveCredential(context.Background(), "pve", "cluster", "user@pve!test=value", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	c := Cluster{ID: "cluster", Name: "test", URL: server.URL, SecretID: cred.ID, CAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))}
	_ = save(context.Background(), s.Store, "cluster", c.ID, c, true)
	if err := s.Sync(context.Background(), c.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	actual, err := s.Host(context.Background(), h.ID)
	if err != nil || actual.Node != "new-node" || actual.SecretID != h.SecretID || actual.HostKey != h.HostKey || actual.WorkstationID != "WS-1" || actual.ProvisionedBy != "clone-operation" {
		t.Fatal(actual, err)
	}
}

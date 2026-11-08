package config

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 部署模板也属于安全边界：验证 YAML 有效、资源限制存在、内部端口不公开。
func TestDeploymentExposureAndResourceLimits(t *testing.T) {
	data, err := os.ReadFile("../../../deploy/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	var stack struct {
		Services map[string]struct {
			Ports  []string `yaml:"ports"`
			CPU    string   `yaml:"cpus"`
			Memory string   `yaml:"mem_limit"`
			PIDs   int      `yaml:"pids_limit"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &stack); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"postgres", "control-plane", "admin-web", "gateway"} {
		service, ok := stack.Services[name]
		if !ok || service.CPU == "" || service.Memory == "" || service.PIDs <= 0 {
			t.Errorf("%s missing limits", name)
		}
	}
	if len(stack.Services["postgres"].Ports) != 0 {
		t.Fatal("database port exposed")
	}
	for _, name := range []string{"control-plane", "admin-web"} {
		for _, port := range stack.Services[name].Ports {
			if !strings.Contains(port, "127.0.0.1") {
				t.Fatalf("%s public by default: %s", name, port)
			}
		}
	}
}

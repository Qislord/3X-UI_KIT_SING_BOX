package sub_test

import (
	"strings"
	"testing"

	"github.com/itsnotkubrick/3X-UI_KIT/internal/sub"
	"gopkg.in/yaml.v3"
)

func TestUserAgentDetection(t *testing.T) {
	clashVerge := "ClashVerge/1.5.0"
	if !sub.IsClashUA(clashVerge) {
		t.Errorf("expected ClashVerge to be Clash UA")
	}
	if !sub.CanAmneziaWG(clashVerge) {
		t.Errorf("expected ClashVerge to support AmneziaWG")
	}

	karing := "Karing/1.0.0 (Clash)"
	if !sub.IsClashUA(karing) {
		t.Errorf("expected Karing with Clash UA to match Clash")
	}
	if sub.CanAmneziaWG(karing) {
		t.Errorf("expected Karing NOT to support AmneziaWG")
	}

	happ := "Happ/3.2.0"
	if sub.IsClashUA(happ) {
		t.Errorf("expected Happ NOT to be Clash UA")
	}
}

func TestFixUserinfo(t *testing.T) {
	in := "upload=1024; download=2048; total=10737418240; expire=0"
	out := sub.FixUserinfo(in)
	expected := "upload=1024; download=2048; total=10737418240"
	if out != expected {
		t.Errorf("expected %s, got %s", expected, out)
	}
}

func TestStripLinks(t *testing.T) {
	plain := "vless://server1\nvpn://amnezia-key\ntg://proxy-link\nhy2://server2\n"
	cleaned := string(sub.StripLinks([]byte(plain)))
	if strings.Contains(cleaned, "vpn://") || strings.Contains(cleaned, "tg://") {
		t.Errorf("links not stripped: %s", cleaned)
	}
	if !strings.Contains(cleaned, "vless://server1") || !strings.Contains(cleaned, "hy2://server2") {
		t.Errorf("valid links missing: %s", cleaned)
	}
}

func TestStripAWG(t *testing.T) {
	sampleYAML := `
proxies:
  - name: "VLESS-Server"
    type: vless
    server: 1.1.1.1
  - name: "AmneziaWG-Server"
    type: wireguard
    server: 2.2.2.2
    amnezia-wg-option:
      jc: 3
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - VLESS-Server
      - AmneziaWG-Server
      - DIRECT
`
	stripped, err := sub.StripAWG([]byte(sampleYAML))
	if err != nil {
		t.Fatalf("StripAWG failed: %v", err)
	}

	var root map[string]interface{}
	if err := yaml.Unmarshal(stripped, &root); err != nil {
		t.Fatalf("failed to unmarshal stripped YAML: %v", err)
	}

	proxies := root["proxies"].([]interface{})
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}
	p0 := proxies[0].(map[string]interface{})
	if p0["name"] != "VLESS-Server" {
		t.Errorf("expected VLESS-Server, got %v", p0["name"])
	}

	groups := root["proxy-groups"].([]interface{})
	g0 := groups[0].(map[string]interface{})
	gp := g0["proxies"].([]interface{})
	for _, px := range gp {
		if px == "AmneziaWG-Server" {
			t.Errorf("AmneziaWG-Server was not stripped from group: %+v", gp)
		}
	}
}

func TestMergeAWG(t *testing.T) {
	mainYAML := `
proxies:
  - name: "VLESS-1"
    type: vless
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - VLESS-1
      - DIRECT
`
	awgYAML := `
proxies:
  - name: "AmneziaWG-3.1-sasha-awg"
    type: wireguard
`
	merged, err := sub.MergeAWG([]byte(mainYAML), []byte(awgYAML))
	if err != nil {
		t.Fatalf("MergeAWG failed: %v", err)
	}

	var root map[string]interface{}
	if err := yaml.Unmarshal(merged, &root); err != nil {
		t.Fatalf("failed to unmarshal merged YAML: %v", err)
	}

	proxies := root["proxies"].([]interface{})
	if len(proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(proxies))
	}

	p1 := proxies[1].(map[string]interface{})
	if p1["name"] != "AmneziaWG-3.1" {
		t.Errorf("expected tail to be stripped, got %v", p1["name"])
	}
}

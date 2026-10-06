package singbox_test

import (
	"testing"

	"github.com/itsnotkubrick/3X-UI_KIT/internal/singbox"
)

func TestGenerateConfig(t *testing.T) {
	rawSub := `
vless://b8313e1d-4464-42b5-829b-57774ae72be5@example.com:443?security=reality&sni=dl.google.com&pbk=123&sid=abc#Reality-Server
hy2://secretPassword@vpn.example.com:443#Hysteria2-Server
tg://proxy?server=1.2.3.4&port=443&secret=123
vpn://amnezia-config
`
	cfg, err := singbox.GenerateConfig(rawSub)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	outbounds, ok := cfg["outbounds"].([]map[string]interface{})
	if !ok || len(outbounds) < 5 {
		t.Fatalf("expected at least 5 outbounds, got %+v", outbounds)
	}

	// First two should be selector and urltest
	if outbounds[0]["type"] != "selector" {
		t.Errorf("expected first outbound to be selector, got %v", outbounds[0]["type"])
	}
	if outbounds[1]["type"] != "urltest" {
		t.Errorf("expected second outbound to be urltest, got %v", outbounds[1]["type"])
	}

	// Check dns
	dns, ok := cfg["dns"].(map[string]interface{})
	if !ok || dns["final"] != "dns-remote" {
		t.Errorf("expected dns-remote final, got %+v", dns)
	}

	// Check inbounds
	inbounds, ok := cfg["inbounds"].([]map[string]interface{})
	if !ok || len(inbounds) == 0 || inbounds[0]["type"] != "tun" {
		t.Errorf("expected tun inbound, got %+v", inbounds)
	}
}

package proxy_test

import (
	"testing"

	"github.com/itsnotkubrick/3X-UI_KIT/internal/proxy"
)

func TestParseVlessReality(t *testing.T) {
	link := "vless://b8313e1d-4464-42b5-829b-57774ae72be5@example.com:443?security=reality&sni=dl.google.com&fp=chrome&pbk=123456&sid=abcdef&type=tcp&flow=xtls-rprx-vision#Reality-Direct"
	out := proxy.ParseProxyLink(link)
	if out == nil {
		t.Fatalf("failed to parse VLESS reality link")
	}

	if out["type"] != "vless" {
		t.Errorf("expected type vless, got %v", out["type"])
	}
	if out["tag"] != "Reality-Direct" {
		t.Errorf("expected tag Reality-Direct, got %v", out["tag"])
	}
	if out["uuid"] != "b8313e1d-4464-42b5-829b-57774ae72be5" {
		t.Errorf("expected uuid match, got %v", out["uuid"])
	}
	tls, ok := out["tls"].(map[string]interface{})
	if !ok || tls["server_name"] != "dl.google.com" {
		t.Errorf("invalid tls settings: %+v", tls)
	}
	reality, ok := tls["reality"].(map[string]interface{})
	if !ok || reality["public_key"] != "123456" || reality["short_id"] != "abcdef" {
		t.Errorf("invalid reality settings: %+v", reality)
	}
}

func TestParseHysteria2(t *testing.T) {
	link := "hy2://secretPassword@vpn.example.com:443?sni=vpn.example.com&insecure=0#Hysteria2"
	out := proxy.ParseProxyLink(link)
	if out == nil {
		t.Fatalf("failed to parse Hysteria2 link")
	}
	if out["type"] != "hysteria2" {
		t.Errorf("expected type hysteria2, got %v", out["type"])
	}
	if out["password"] != "secretPassword" {
		t.Errorf("expected password match, got %v", out["password"])
	}
}

func TestParseShadowsocks(t *testing.T) {
	link := "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@1.2.3.4:8388#MySS"
	out := proxy.ParseProxyLink(link)
	if out == nil {
		t.Fatalf("failed to parse Shadowsocks link")
	}
	if out["type"] != "shadowsocks" {
		t.Errorf("expected type shadowsocks, got %v", out["type"])
	}
	if out["method"] != "aes-256-gcm" || out["password"] != "password" {
		t.Errorf("invalid ss credentials: %+v", out)
	}
	if out["server"] != "1.2.3.4" || out["server_port"] != 8388 {
		t.Errorf("invalid server/port: %+v", out)
	}
}

func TestFixTGLink(t *testing.T) {
	orig := "tg://proxy?server=127.0.0.1&port=10445&secret=ee123"
	fixed := proxy.FixTGLink(orig, "vpn.test.ru")
	expected := "tg://proxy?server=vpn.test.ru&port=443&secret=ee123"
	if fixed != expected {
		t.Errorf("expected %s, got %s", expected, fixed)
	}
}

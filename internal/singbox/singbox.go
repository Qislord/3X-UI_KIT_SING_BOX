package singbox

import (
	"errors"
	"fmt"
	"strings"

	"github.com/itsnotkubrick/3X-UI_KIT/internal/proxy"
)

var ErrNoCompatibleProtocols = errors.New("на сервере не найдено совместимых протоколов для Sing-box")

// GenerateConfig parses raw subscription lines and builds a Sing-box 1.9+ JSON config.
func GenerateConfig(rawSub string) (map[string]interface{}, error) {
	var outboundsList []map[string]interface{}
	var tags []string
	tagSet := make(map[string]bool)

	lines := strings.Split(rawSub, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "vpn://") || strings.HasPrefix(line, "tg://") {
			continue
		}

		p := proxy.ParseProxyLink(line)
		if p != nil {
			tag, _ := p["tag"].(string)
			if tag == "" {
				tag = "Прокси"
			}

			baseTag := tag
			idx := 2
			for tagSet[tag] {
				tag = fmt.Sprintf("%s %d", baseTag, idx)
				idx++
			}
			p["tag"] = tag
			tagSet[tag] = true
			tags = append(tags, tag)
			outboundsList = append(outboundsList, p)
		}
	}

	if len(outboundsList) == 0 {
		return nil, ErrNoCompatibleProtocols
	}

	selectorGroup := "Выбор подключения"
	autoGroup := "Авто (быстрый)"

	var selectorOutbounds []string
	selectorOutbounds = append(selectorOutbounds, autoGroup)
	selectorOutbounds = append(selectorOutbounds, tags...)
	selectorOutbounds = append(selectorOutbounds, "direct", "block")

	allOutbounds := []map[string]interface{}{
		{
			"type":      "selector",
			"tag":       selectorGroup,
			"outbounds": selectorOutbounds,
			"default":   autoGroup,
		},
		{
			"type":      "urltest",
			"tag":       autoGroup,
			"outbounds": tags,
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "3m",
			"tolerance": 50,
		},
	}
	allOutbounds = append(allOutbounds, outboundsList...)
	allOutbounds = append(allOutbounds,
		map[string]interface{}{
			"type": "direct",
			"tag":  "direct",
		},
		map[string]interface{}{
			"type": "block",
			"tag":  "block",
		},
		map[string]interface{}{
			"type": "dns",
			"tag":  "dns-out",
		},
	)

	config := map[string]interface{}{
		"log": map[string]interface{}{
			"level":     "warn",
			"timestamp": true,
		},
		"dns": map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"tag":              "dns-remote",
					"address":          "https://1.1.1.1/dns-query",
					"address_resolver": "dns-direct",
					"strategy":         "prefer_ipv4",
					"detour":           selectorGroup,
				},
				{
					"tag":              "dns-backup",
					"address":          "https://8.8.8.8/dns-query",
					"address_resolver": "dns-direct",
					"strategy":         "prefer_ipv4",
					"detour":           selectorGroup,
				},
				{
					"tag":              "dns-direct",
					"address":          "https://77.88.8.8/dns-query",
					"address_resolver": "dns-local",
					"strategy":         "prefer_ipv4",
					"detour":           "direct",
				},
				{
					"tag":     "dns-local",
					"address": "local",
					"detour":  "direct",
				},
				{
					"tag":     "dns-block",
					"address": "rcode://success",
				},
			},
			"rules": []map[string]interface{}{
				{
					"outbound": "any",
					"server":   "dns-direct",
				},
				{
					"geosite": "category-ads-all",
					"server":  "dns-block",
				},
				{
					"geosite": []string{"category-ru", "ru"},
					"server":  "dns-direct",
				},
			},
			"final":    "dns-remote",
			"strategy": "prefer_ipv4",
		},
		"inbounds": []map[string]interface{}{
			{
				"type":                       "tun",
				"tag":                        "tun-in",
				"interface_name":             "tun0",
				"inet4_address":              "172.19.0.1/30",
				"auto_route":                 true,
				"strict_route":               true,
				"stack":                      "mixed",
				"sniff":                      true,
				"sniff_override_destination": false,
			},
		},
		"outbounds": allOutbounds,
		"route": map[string]interface{}{
			"auto_detect_interface": true,
			"final":                 selectorGroup,
			"rules": []map[string]interface{}{
				{"protocol": "dns", "outbound": "dns-out"},
				{"port": 53, "outbound": "dns-out"},
				{"protocol": "bittorrent", "outbound": "direct"},
				{"network": "udp", "port": 443, "outbound": "block"},
				{"ip_is_private": true, "outbound": "direct"},
				{"geoip": "private", "outbound": "direct"},
				{"geosite": "private", "outbound": "direct"},
				{"geosite": []string{"category-ru", "ru"}, "outbound": "direct"},
				{"geoip": "ru", "outbound": "direct"},
				{"geosite": []string{"category-ads-all"}, "outbound": "block"},
			},
		},
		"experimental": map[string]interface{}{
			"cache_file": map[string]interface{}{
				"enabled":       true,
				"store_fakeip": false,
			},
		},
	}

	return config, nil
}

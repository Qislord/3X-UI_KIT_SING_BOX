package proxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ProxyInfo represents a parsed proxy for display in user portal.
type ProxyInfo struct {
	Tag  string `json:"tag"`
	Type string `json:"type"`
	Link string `json:"link"`
}

// ParseProxyInfo extracts human-readable tag and type from a URI.
func ParseProxyInfo(link string) *ProxyInfo {
	link = strings.TrimSpace(link)
	if link == "" || strings.HasPrefix(link, "#") {
		return nil
	}

	if strings.HasPrefix(strings.ToLower(link), "vpn://") {
		return parseAmneziaVPNInfo(link)
	}

	outbound := ParseProxyLink(link)
	if outbound != nil {
		tag, _ := outbound["tag"].(string)
		typ, _ := outbound["type"].(string)
		if tag == "" {
			tag = "Прокси"
		}
		return &ProxyInfo{
			Tag:  tag,
			Type: strings.ToUpper(typ),
			Link: link,
		}
	}

	if idx := strings.Index(link, "://"); idx != -1 {
		scheme := strings.ToUpper(link[:idx])
		frag := scheme
		if hIdx := strings.Index(link, "#"); hIdx != -1 {
			if decoded, err := url.PathUnescape(link[hIdx+1:]); err == nil && decoded != "" {
				frag = decoded
			}
		}
		return &ProxyInfo{
			Tag:  frag,
			Type: scheme,
			Link: link,
		}
	}
	return nil
}

// parseAmneziaVPNInfo extracts info from vpn:// links containing AmneziaWG / WireGuard config.
func parseAmneziaVPNInfo(link string) *ProxyInfo {
	payload := link[len("vpn://"):]
	frag := ""
	if idx := strings.Index(payload, "#"); idx != -1 {
		if decoded, err := url.PathUnescape(payload[idx+1:]); err == nil && decoded != "" {
			frag = decoded
		} else {
			frag = payload[idx+1:]
		}
		payload = payload[:idx]
	}

	decoded, err := decodeBase64Safe(payload)
	if err == nil && strings.Contains(decoded, "[Interface]") {
		lowerDec := strings.ToLower(decoded)
		hasH := strings.Contains(lowerDec, "h1") || strings.Contains(lowerDec, "h2") || strings.Contains(lowerDec, "h3") || strings.Contains(lowerDec, "h4")
		hasJ := strings.Contains(lowerDec, "jc") || strings.Contains(lowerDec, "jmin") || strings.Contains(lowerDec, "jmax") || strings.Contains(lowerDec, "s1") || strings.Contains(lowerDec, "s2")

		tag := frag
		if tag == "" || strings.EqualFold(tag, "vpn") {
			if hasH {
				tag = "AmneziaWG (3.1 - защита заголовков)"
			} else if hasJ {
				tag = "AmneziaWG (Классика)"
			} else {
				tag = "AmneziaWG"
			}
		}

		return &ProxyInfo{
			Tag:  tag,
			Type: "AMNEZIA",
			Link: link,
		}
	}

	tag := frag
	if tag == "" || strings.EqualFold(tag, "vpn") {
		tag = "AmneziaWG"
	}
	return &ProxyInfo{
		Tag:  tag,
		Type: "AMNEZIA",
		Link: link,
	}
}

// ParseProxyLink parses supported proxy links into Sing-box outbound map.
func ParseProxyLink(link string) map[string]interface{} {
	link = strings.TrimSpace(link)
	if link == "" || strings.HasPrefix(link, "#") {
		return nil
	}

	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	scheme := strings.ToLower(u.Scheme)
	tag := u.Fragment
	if tag != "" {
		if unescaped, err := url.PathUnescape(tag); err == nil {
			tag = unescaped
		}
	} else {
		tag = fmt.Sprintf("%s-%s:%s", strings.ToUpper(scheme), u.Hostname(), u.Port())
	}

	q := u.Query()
	getParam := func(key string) string {
		return q.Get(key)
	}

	port := 443
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
		port = p
	}

	switch scheme {
	case "vless":
		uuid := u.User.Username()
		security := strings.ToLower(getParam("security"))
		network := strings.ToLower(getParam("type"))
		if network == "" {
			network = "tcp"
		}
		flow := getParam("flow")

		outbound := map[string]interface{}{
			"type":        "vless",
			"tag":         tag,
			"server":      u.Hostname(),
			"server_port": port,
			"uuid":        uuid,
		}
		if flow != "" {
			outbound["flow"] = flow
		}

		if security == "tls" || security == "reality" {
			tls := map[string]interface{}{
				"enabled": true,
			}
			sni := getParam("sni")
			if sni == "" {
				sni = getParam("host")
			}
			if sni == "" {
				sni = u.Hostname()
			}
			tls["server_name"] = sni

			fp := getParam("fp")
			if fp == "" {
				fp = "chrome"
			}
			tls["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": fp,
			}

			if security == "reality" {
				tls["reality"] = map[string]interface{}{
					"enabled":    true,
					"public_key": getParam("pbk"),
					"short_id":   getParam("sid"),
				}
			}

			if alpn := getParam("alpn"); alpn != "" {
				var alpnList []string
				for _, item := range strings.Split(alpn, ",") {
					if trimmed := strings.TrimSpace(item); trimmed != "" {
						alpnList = append(alpnList, trimmed)
					}
				}
				if len(alpnList) > 0 {
					tls["alpn"] = alpnList
				}
			}
			outbound["tls"] = tls
		}

		switch network {
		case "ws":
			transport := map[string]interface{}{
				"type": "ws",
				"path": getParam("path"),
			}
			if transport["path"] == "" {
				transport["path"] = "/"
			}
			if host := getParam("host"); host != "" {
				transport["headers"] = map[string]string{"Host": host}
			}
			outbound["transport"] = transport
		case "grpc":
			svc := getParam("serviceName")
			if svc == "" {
				svc = getParam("path")
			}
			outbound["transport"] = map[string]interface{}{
				"type":         "grpc",
				"service_name": svc,
			}
		case "httpupgrade", "xhttp":
			path := getParam("path")
			if path == "" {
				path = "/"
			}
			transport := map[string]interface{}{
				"type": network,
				"path": path,
			}
			if host := getParam("host"); host != "" {
				transport["host"] = host
			}
			outbound["transport"] = transport
		}
		return outbound

	case "hy2", "hysteria2":
		auth := u.User.Username()
		if pwd, ok := u.User.Password(); ok && pwd != "" {
			auth += ":" + pwd
		}
		sni := getParam("sni")
		if sni == "" {
			sni = u.Hostname()
		}
		insecure := getParam("insecure") == "1" || strings.ToLower(getParam("insecure")) == "true"

		tls := map[string]interface{}{
			"enabled":     true,
			"server_name": sni,
		}
		if insecure {
			tls["insecure"] = true
		}
		if alpn := getParam("alpn"); alpn != "" {
			var alpnList []string
			for _, item := range strings.Split(alpn, ",") {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					alpnList = append(alpnList, trimmed)
				}
			}
			if len(alpnList) > 0 {
				tls["alpn"] = alpnList
			}
		}

		outbound := map[string]interface{}{
			"type":        "hysteria2",
			"tag":         tag,
			"server":      u.Hostname(),
			"server_port": port,
			"password":    auth,
			"tls":         tls,
		}
		if obfs := getParam("obfs"); obfs != "" {
			outbound["obfs"] = map[string]interface{}{
				"type":     obfs,
				"password": getParam("obfs-password"),
			}
		}
		return outbound

	case "tuic":
		uuid := u.User.Username()
		pwd, _ := u.User.Password()
		sni := getParam("sni")
		if sni == "" {
			sni = u.Hostname()
		}

		alpnList := []string{"h3"}
		if alpn := getParam("alpn"); alpn != "" {
			alpnList = nil
			for _, item := range strings.Split(alpn, ",") {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					alpnList = append(alpnList, trimmed)
				}
			}
		}

		cc := getParam("congestion_control")
		if cc == "" {
			cc = "bbr"
		}
		udpRelay := getParam("udp_relay_mode")
		if udpRelay == "" {
			udpRelay = "native"
		}

		return map[string]interface{}{
			"type":               "tuic",
			"tag":                tag,
			"server":             u.Hostname(),
			"server_port":        port,
			"uuid":               uuid,
			"password":           pwd,
			"congestion_control": cc,
			"udp_relay_mode":     udpRelay,
			"tls": map[string]interface{}{
				"enabled":     true,
				"server_name": sni,
				"alpn":        alpnList,
			},
		}

	case "trojan":
		pwd := u.User.Username()
		sni := getParam("sni")
		if sni == "" {
			sni = u.Hostname()
		}

		tls := map[string]interface{}{
			"enabled":     true,
			"server_name": sni,
		}
		if alpn := getParam("alpn"); alpn != "" {
			var alpnList []string
			for _, item := range strings.Split(alpn, ",") {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					alpnList = append(alpnList, trimmed)
				}
			}
			if len(alpnList) > 0 {
				tls["alpn"] = alpnList
			}
		}

		outbound := map[string]interface{}{
			"type":        "trojan",
			"tag":         tag,
			"server":      u.Hostname(),
			"server_port": port,
			"password":    pwd,
			"tls":         tls,
		}

		network := strings.ToLower(getParam("type"))
		if network == "grpc" {
			svc := getParam("serviceName")
			if svc == "" {
				svc = getParam("path")
			}
			outbound["transport"] = map[string]interface{}{
				"type":         "grpc",
				"service_name": svc,
			}
		} else if network == "ws" {
			path := getParam("path")
			if path == "" {
				path = "/"
			}
			outbound["transport"] = map[string]interface{}{
				"type": "ws",
				"path": path,
			}
		}
		return outbound

	case "ss":
		body := link[len("ss://"):]
		frag := ""
		if idx := strings.Index(body, "#"); idx != -1 {
			frag = body[idx+1:]
			body = body[:idx]
			if unescaped, err := url.PathUnescape(frag); err == nil {
				tag = unescaped
			} else {
				tag = frag
			}
		}
		if idx := strings.Index(body, "?"); idx != -1 {
			body = body[:idx]
		}
		body = strings.TrimRight(body, "/")

		var userinfo, hostport string
		if strings.Contains(body, "@") {
			parts := strings.SplitN(body, "@", 2)
			userinfo = parts[0]
			hostport = parts[1]
			if !strings.Contains(userinfo, ":") {
				if decoded, err := decodeBase64Safe(userinfo); err == nil {
					userinfo = decoded
				}
			}
		} else {
			if decoded, err := decodeBase64Safe(body); err == nil && strings.Contains(decoded, "@") {
				parts := strings.SplitN(decoded, "@", 2)
				userinfo = parts[0]
				hostport = parts[1]
			} else {
				return nil
			}
		}

		uParts := strings.SplitN(userinfo, ":", 2)
		if len(uParts) != 2 {
			return nil
		}
		hParts := strings.SplitN(hostport, ":", 2)
		if len(hParts) != 2 {
			return nil
		}
		ssPort, _ := strconv.Atoi(hParts[1])
		return map[string]interface{}{
			"type":        "shadowsocks",
			"tag":         tag,
			"server":      hParts[0],
			"server_port": ssPort,
			"method":      strings.ToLower(uParts[0]),
			"password":    uParts[1],
		}

	case "vmess":
		body := link[len("vmess://"):]
		if idx := strings.Index(body, "#"); idx != -1 {
			body = body[:idx]
		}
		decoded, err := decodeBase64Safe(body)
		if err != nil {
			return nil
		}
		var j map[string]interface{}
		if err := json.Unmarshal([]byte(decoded), &j); err != nil {
			return nil
		}

		vTag := tag
		if ps, ok := j["ps"].(string); ok && ps != "" {
			vTag = ps
		}

		vPort := 443
		if pVal, ok := j["port"]; ok {
			switch p := pVal.(type) {
			case float64:
				vPort = int(p)
			case string:
				if v, err := strconv.Atoi(p); err == nil {
					vPort = v
				}
			}
		}

		aid := 0
		if aVal, ok := j["aid"]; ok {
			switch a := aVal.(type) {
			case float64:
				aid = int(a)
			case string:
				aid, _ = strconv.Atoi(a)
			}
		}

		scy, _ := j["scy"].(string)
		if scy == "" {
			scy = "auto"
		}

		outbound := map[string]interface{}{
			"type":        "vmess",
			"tag":         vTag,
			"server":      j["add"],
			"server_port": vPort,
			"uuid":        j["id"],
			"security":    scy,
			"alter_id":    aid,
		}

		tlsVal, _ := j["tls"].(string)
		if tlsVal == "tls" || tlsVal == "reality" {
			sni, _ := j["sni"].(string)
			if sni == "" {
				sni, _ = j["host"].(string)
			}
			if sni == "" {
				sni, _ = j["add"].(string)
			}
			outbound["tls"] = map[string]interface{}{
				"enabled":     true,
				"server_name": sni,
			}
		}

		netVal, _ := j["net"].(string)
		netVal = strings.ToLower(netVal)
		if netVal == "ws" {
			path, _ := j["path"].(string)
			if path == "" {
				path = "/"
			}
			outbound["transport"] = map[string]interface{}{
				"type": "ws",
				"path": path,
			}
		} else if netVal == "grpc" {
			path, _ := j["path"].(string)
			outbound["transport"] = map[string]interface{}{
				"type":         "grpc",
				"service_name": path,
			}
		}
		return outbound
	}

	return nil
}

// FixTGLink ensures Telegram MTProto link uses public port 443 instead of internal 10445.
func FixTGLink(link, host string) string {
	if !strings.HasPrefix(link, "tg://") {
		return link
	}
	rePort := regexp.MustCompile(`([?&]port=)10445\b`)
	link = rePort.ReplaceAllString(link, "${1}443")

	if host != "" {
		reHost := regexp.MustCompile(`([?&]server=)(127\.0\.0\.1|localhost)\b`)
		link = reHost.ReplaceAllString(link, fmt.Sprintf("${1}%s", host))
	}
	return link
}

// FixWSLink updates the server address, host and sni for WebSocket proxies (VMess/VLESS)
// to use a dedicated CDN/Cloudflare domain (wsDomain).
func FixWSLink(link, wsDomain string) string {
	if wsDomain == "" || link == "" {
		return link
	}

	if strings.HasPrefix(link, "vmess://") {
		body := link[len("vmess://"):]
		frag := ""
		if idx := strings.Index(body, "#"); idx != -1 {
			frag = body[idx:]
			body = body[:idx]
		}
		decoded, err := decodeBase64Safe(body)
		if err != nil {
			return link
		}
		var j map[string]interface{}
		if err := json.Unmarshal([]byte(decoded), &j); err != nil {
			return link
		}
		netVal, _ := j["net"].(string)
		if strings.ToLower(netVal) == "ws" {
			j["add"] = wsDomain
			j["host"] = wsDomain
			if tlsVal, ok := j["tls"].(string); ok && (tlsVal == "tls" || tlsVal == "reality") {
				j["sni"] = wsDomain
			}
			data, err := json.Marshal(j)
			if err != nil {
				return link
			}
			return "vmess://" + base64.StdEncoding.EncodeToString(data) + frag
		}
		return link
	}

	if strings.HasPrefix(link, "vless://") {
		u, err := url.Parse(link)
		if err != nil {
			return link
		}
		q := u.Query()
		if strings.ToLower(q.Get("type")) == "ws" {
			port := u.Port()
			if port == "" {
				port = "443"
			}
			u.Host = fmt.Sprintf("%s:%s", wsDomain, port)
			q.Set("host", wsDomain)
			sec := strings.ToLower(q.Get("security"))
			if sec == "tls" || sec == "reality" {
				q.Set("sni", wsDomain)
			}
			u.RawQuery = q.Encode()
			return u.String()
		}
		return link
	}

	if strings.HasPrefix(link, "trojan://") {
		u, err := url.Parse(link)
		if err != nil {
			return link
		}
		q := u.Query()
		if strings.ToLower(q.Get("type")) == "ws" {
			port := u.Port()
			if port == "" {
				port = "443"
			}
			u.Host = fmt.Sprintf("%s:%s", wsDomain, port)
			q.Set("host", wsDomain)
			sec := strings.ToLower(q.Get("security"))
			if sec == "tls" || sec == "reality" {
				q.Set("sni", wsDomain)
			}
			u.RawQuery = q.Encode()
			return u.String()
		}
		return link
	}

	return link
}

func decodeBase64Safe(s string) (string, error) {
	s = strings.TrimSpace(s)
	// Add padding if missing
	if rem := len(s) % 4; rem > 0 {
		s += strings.Repeat("=", 4-rem)
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(s)
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

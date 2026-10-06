package sub

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	ReClashUA = regexp.MustCompile(`(?i)clash|mihomo|flclash|stash|nyanpasu|meta`)
	ReNoAwgUA = regexp.MustCompile(`(?i)karing|hiddify|nekobox|sing-?box|husi|stash|shadowrocket|v2box|streisand|happ|loon|surge|quantumult`)
	ReSubID   = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,64}$`)
	ReAwgTail = regexp.MustCompile(`-[^-\s]+-awg\d*$`)

	HappHeaders = []string{
		"routing", "routing-enable", "announce", "providerid", "new-url", "fallback-url",
		"hide-settings", "no-limit-enabled", "ping-type", "color-profile", "tun-mode", "tun-type",
		"exclude-routes", "exclude-apns-enable", "per-app-proxy-mode", "per-app-proxy-list",
		"notification-subs-expire", "sub-expire", "sub-expire-button-link", "sub-info-text",
		"sub-info-color", "sub-info-button-text", "sub-info-button-link",
		"subscription-autoconnect", "subscription-autoconnect-type", "subscription-always-hwid-enable",
	}

	PassHeaders = append([]string{
		"content-type", "content-disposition", "profile-title", "profile-update-interval",
		"profile-web-page-url", "subscription-userinfo", "support-url", "cache-control",
	}, HappHeaders...)
)

// UpstreamResponse holds upstream HTTP status, headers and body.
type UpstreamResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// Client fetches subscriptions from internal 3X-UI service.
type Client struct {
	httpClient *http.Client
}

func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
	}
}

// FetchUpstream requests the raw subscription from 3X-UI internal endpoint.
func (c *Client) FetchUpstream(baseURL, subPath, subID, ua, host, accept string) (*UpstreamResponse, error) {
	reqURL := fmt.Sprintf("%s/%s/%s", strings.TrimRight(baseURL, "/"), strings.Trim(subPath, "/"), subID)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if host != "" {
		req.Host = host
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	} else {
		req.Header.Set("Accept", "*/*")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = v[0]
		}
	}

	return &UpstreamResponse{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       body,
	}, nil
}

// FixUserinfo removes "expire=0" so client apps don't display 01.01.1970.
func FixUserinfo(value string) string {
	parts := strings.Split(value, ";")
	var clean []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" && trimmed != "expire=0" {
			clean = append(clean, trimmed)
		}
	}
	return strings.Join(clean, "; ")
}

// StripLinks removes vpn:// (AmneziaVPN) and tg:// (Telegram) links from raw text or base64 subscription.
func StripLinks(body []byte) []byte {
	text := string(body)
	text = strings.TrimSpace(text)
	isBase64 := !strings.Contains(text, "://")

	if isBase64 {
		decoded, err := decodeBase64(text)
		if err == nil {
			text = string(decoded)
		} else {
			return body
		}
	}

	lines := strings.Split(text, "\n")
	var clean []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" && !strings.HasPrefix(trimmed, "vpn://") && !strings.HasPrefix(trimmed, "tg://") {
			clean = append(clean, trimmed)
		}
	}

	out := strings.Join(clean, "\n")
	if isBase64 {
		return []byte(base64.StdEncoding.EncodeToString([]byte(out)))
	}
	return []byte(out)
}

// StripAWG removes AmneziaWG proxies from Clash YAML for apps that do not support AWG.
func StripAWG(clashYAML []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(clashYAML, &doc); err != nil {
		return clashYAML, err
	}

	var root map[string]interface{}
	if err := yaml.Unmarshal(clashYAML, &root); err != nil {
		return clashYAML, err
	}

	proxies, ok := root["proxies"].([]interface{})
	if !ok || len(proxies) == 0 {
		return clashYAML, nil
	}

	awgNames := make(map[string]bool)
	var filteredProxies []interface{}

	for _, p := range proxies {
		pm, ok := p.(map[string]interface{})
		if !ok {
			filteredProxies = append(filteredProxies, p)
			continue
		}
		name, _ := pm["name"].(string)
		if _, hasOpt := pm["amnezia-wg-option"]; hasOpt {
			if name != "" {
				awgNames[name] = true
			}
			continue
		}
		filteredProxies = append(filteredProxies, p)
	}

	if len(awgNames) == 0 {
		return clashYAML, nil
	}

	root["proxies"] = filteredProxies

	if groups, ok := root["proxy-groups"].([]interface{}); ok {
		for _, g := range groups {
			gm, ok := g.(map[string]interface{})
			if !ok {
				continue
			}
			gp, ok := gm["proxies"].([]interface{})
			if !ok {
				continue
			}
			var cleanGP []interface{}
			for _, px := range gp {
				pxStr, _ := px.(string)
				if !awgNames[pxStr] {
					cleanGP = append(cleanGP, px)
				}
			}
			gm["proxies"] = cleanGP
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return clashYAML, err
	}
	return buf.Bytes(), nil
}

// MergeAWG merges AmneziaWG proxies from awgYAML into mainYAML.
func MergeAWG(mainYAML, awgYAML []byte) ([]byte, error) {
	var mainRoot, awgRoot map[string]interface{}
	if err := yaml.Unmarshal(mainYAML, &mainRoot); err != nil {
		return mainYAML, err
	}
	if err := yaml.Unmarshal(awgYAML, &awgRoot); err != nil {
		return mainYAML, err
	}

	awgProxies, ok := awgRoot["proxies"].([]interface{})
	if !ok || len(awgProxies) == 0 {
		return mainYAML, nil
	}

	existingNames := make(map[string]bool)
	mainProxies, _ := mainRoot["proxies"].([]interface{})
	for _, p := range mainProxies {
		if pm, ok := p.(map[string]interface{}); ok {
			if name, ok := pm["name"].(string); ok {
				existingNames[name] = true
			}
		}
	}

	var extraProxies []interface{}
	var addedNames []string

	for _, p := range awgProxies {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := pm["name"].(string)
		if name == "" {
			continue
		}

		name = ReAwgTail.ReplaceAllString(name, "")
		base := name
		idx := 2
		for existingNames[name] {
			name = fmt.Sprintf("%s %d", base, idx)
			idx++
		}
		pm["name"] = name
		existingNames[name] = true
		extraProxies = append(extraProxies, pm)
		addedNames = append(addedNames, name)
	}

	if len(extraProxies) == 0 {
		return mainYAML, nil
	}

	mainRoot["proxies"] = append(mainProxies, extraProxies...)

	if groups, ok := mainRoot["proxy-groups"].([]interface{}); ok {
		for _, g := range groups {
			gm, ok := g.(map[string]interface{})
			if !ok {
				continue
			}
			gp, ok := gm["proxies"].([]interface{})
			if !ok {
				continue
			}

			hasKnownProxy := false
			directPos := -1
			for i, px := range gp {
				pxStr, _ := px.(string)
				if existingNames[pxStr] {
					hasKnownProxy = true
				}
				if pxStr == "DIRECT" && directPos == -1 {
					directPos = i
				}
			}

			if hasKnownProxy {
				var newGP []interface{}
				if directPos != -1 {
					newGP = append(newGP, gp[:directPos]...)
					for _, n := range addedNames {
						newGP = append(newGP, n)
					}
					newGP = append(newGP, gp[directPos:]...)
				} else {
					newGP = append(newGP, gp...)
					for _, n := range addedNames {
						newGP = append(newGP, n)
					}
				}
				gm["proxies"] = newGP
			}
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(mainRoot); err != nil {
		return mainYAML, err
	}
	return buf.Bytes(), nil
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if rem := len(s) % 4; rem > 0 {
		s += strings.Repeat("=", 4-rem)
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(s)
	}
	return decoded, err
}

// IsClashUA checks if client is a Clash/Mihomo variant.
func IsClashUA(ua string) bool {
	return ReClashUA.MatchString(ua)
}

// CanAmneziaWG checks if UA supports AmneziaWG (Mihomo core, but not sing-box based clients).
func CanAmneziaWG(ua string) bool {
	return IsClashUA(ua) && !ReNoAwgUA.MatchString(ua)
}

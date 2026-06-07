package tusupload

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"ch/kirari04/videocms/config"
)

func canonicalTusOrigin(r *http.Request) *url.URL {
	scheme := "http"
	host := ""
	if r != nil {
		if r.TLS != nil {
			scheme = "https"
		}
		if proto := firstHeaderValue(r.Header.Get("X-Forwarded-Proto")); proto == "http" || proto == "https" {
			scheme = proto
		}
		host = r.Host
		if host == "" {
			host = r.URL.Host
		}
	}

	baseURL := publicBaseURL()
	if baseURL != nil {
		if host == "" || isInternalHost(hostnameOnly(host)) || sameHostname(host, baseURL.Host) {
			scheme = baseURL.Scheme
			host = baseURL.Host
		}
	}

	if host == "" {
		return nil
	}

	return &url.URL{
		Scheme: scheme,
		Host:   normalizeHostPort(scheme, host),
	}
}

func rewriteTusUploadURL(r *http.Request, raw string) string {
	if raw == "" {
		return raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if !strings.HasPrefix(parsed.Path, BasePath) {
		return raw
	}

	origin := canonicalTusOrigin(r)
	if origin == nil {
		return raw
	}

	parsed.Scheme = origin.Scheme
	parsed.Host = origin.Host
	return parsed.String()
}

func rewriteTusUploadConcatHeader(r *http.Request, raw string) string {
	if raw == "" {
		return raw
	}

	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return raw
	}

	changed := false
	for i, part := range parts {
		if prefix, ok := tusConcatPrefix(part); ok {
			rewrite := prefix + rewriteTusUploadURL(r, strings.TrimPrefix(part, prefix))
			if rewrite != part {
				parts[i] = rewrite
				changed = true
			}
			continue
		}

		rewrite := rewriteTusUploadURL(r, part)
		if rewrite != part {
			parts[i] = rewrite
			changed = true
		}
	}
	if !changed {
		return raw
	}
	return strings.Join(parts, " ")
}

func tusConcatPrefix(part string) (string, bool) {
	for _, prefix := range []string{"partial;", "final;"} {
		if strings.HasPrefix(part, prefix) {
			return prefix, true
		}
	}
	return "", false
}

func publicBaseURL() *url.URL {
	raw := strings.TrimSpace(config.ENV.BaseUrl)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil
	}
	return parsed
}

func firstHeaderValue(raw string) string {
	if raw == "" {
		return ""
	}
	first, _, _ := strings.Cut(raw, ",")
	return strings.ToLower(strings.TrimSpace(first))
}

func hostnameOnly(hostport string) string {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(strings.ToLower(host), "[]")
	}
	return strings.Trim(strings.ToLower(hostport), "[]")
}

func sameHostname(left, right string) bool {
	leftHost := hostnameOnly(left)
	rightHost := hostnameOnly(right)
	return leftHost != "" && rightHost != "" && leftHost == rightHost
}

func isInternalHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}
	return !strings.Contains(host, ".")
}

func normalizeHostPort(scheme, hostport string) string {
	if host, port, err := net.SplitHostPort(hostport); err == nil {
		if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
			if strings.Contains(host, ":") {
				return "[" + strings.Trim(host, "[]") + "]"
			}
			return host
		}
		return net.JoinHostPort(host, port)
	}
	return strings.TrimSpace(hostport)
}

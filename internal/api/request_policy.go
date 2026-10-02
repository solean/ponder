package api

import (
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Desktop asset requests never pass through Run's network listener. Keeping
// these policies separate prevents native origins being trusted over HTTP.
type requestPolicy struct {
	desktop   bool
	bindHost  string
	remoteIPs bool
}

func listenerPolicy(addr string) requestPolicy {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return requestPolicy{}
	} // ListenAndServe will report the invalid address.
	host = strings.ToLower(host)
	ip := net.ParseIP(host)
	return requestPolicy{bindHost: host, remoteIPs: host == "" || (ip != nil && ip.IsUnspecified())}
}

// authorityURL parses only a host and optional numeric port, without accepting
// userinfo, path/query suffixes, or unbracketed IPv6 literals.
func authorityURL(authority string) (*url.URL, bool) {
	u, err := url.Parse("http://" + authority)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(authority, "/#?@\\ \t\r\n") {
		return nil, false
	}
	host := u.Hostname()
	if host == "" {
		return nil, false
	}
	if (strings.HasPrefix(authority, "[") && net.ParseIP(host) == nil) || (strings.Contains(host, ":") && !strings.HasPrefix(authority, "[")) {
		return nil, false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, false
		}
	} else if strings.HasSuffix(authority, ":") {
		return nil, false
	}
	return u, true
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (p requestPolicy) hostAllowed(authority string) bool {
	u, ok := authorityURL(authority)
	if !ok {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if loopbackHost(host) {
		return true
	}
	if p.desktop && (host == "wails.localhost" || host == "wails") {
		return true
	}
	if p.remoteIPs && net.ParseIP(host) != nil {
		return true
	}
	return p.bindHost != "" && host == p.bindHost
}

// Origins must be serialized scheme/host/port tuples. "null" is deliberately
// rejected: it also represents sandboxed documents, not just local files.
func parseOrigin(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Opaque != "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, "#? \t\r\n") {
		return nil, false
	}
	_, ok := authorityURL(u.Host)
	return u, ok
}

func originPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if u.Scheme == "https" {
		return "443"
	}
	if u.Scheme == "http" {
		return "80"
	}
	return ""
}

func (p requestPolicy) originAllowed(raw string, r *http.Request) bool {
	origin, ok := parseOrigin(raw)
	if !ok {
		return false
	}
	if origin.Scheme == "wails" {
		// Wails includes the Vite port in native development URLs. Require
		// the full authority to match the asset request, including that port.
		return p.desktop && (origin.Hostname() == "localhost" || origin.Hostname() == "wails") && strings.EqualFold(origin.Host, r.Host)
	}
	if origin.Scheme != "http" && origin.Scheme != "https" {
		return false
	}
	// All loopback ports support Vite's port fallback and custom local dev ports.
	if loopbackHost(origin.Hostname()) {
		return true
	}
	if !p.hostAllowed(r.Host) {
		return false
	}
	target, _ := authorityURL(r.Host)
	target.Scheme = "http"
	if r.TLS != nil {
		target.Scheme = "https"
	}
	return origin.Scheme == target.Scheme && strings.EqualFold(origin.Hostname(), target.Hostname()) && originPort(origin) == originPort(target)
}

func (p requestPolicy) requestOriginAllowed(r *http.Request) bool {
	origins, present := r.Header["Origin"]
	if present {
		return len(origins) == 1 && p.originAllowed(origins[0], r)
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	// CLI clients and macOS Wails omit Origin. Browser evidence, when present,
	// must still identify a trusted page. Do not trust forwarded host headers.
	if referer := r.Header.Get("Referer"); referer != "" {
		u, err := url.Parse(referer)
		if err != nil || u.User != nil {
			return false
		}
		return p.originAllowed(u.Scheme+"://"+u.Host, r)
	}
	return true
}

func (p requestPolicy) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.hostAllowed(r.Host) {
			writeError(w, http.StatusForbidden, "forbidden host")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Origin")
		w.Header().Add("Vary", "Sec-Fetch-Site")
		w.Header().Add("Vary", "Referer")
		if !p.requestOriginAllowed(r) {
			writeError(w, http.StatusForbidden, "forbidden origin")
			return
		}
		if r.Method == http.MethodOptions {
			if requested := r.Header.Get("Access-Control-Request-Method"); requested != "" && requested != "GET" && requested != "HEAD" && requested != "POST" {
				writeError(w, http.StatusForbidden, "forbidden preflight method")
				return
			}
			for _, header := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
				if strings.TrimSpace(header) != "" && !strings.EqualFold(strings.TrimSpace(header), "Content-Type") {
					writeError(w, http.StatusForbidden, "forbidden preflight header")
					return
				}
			}
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS, POST")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var errUnsupportedMediaType = errors.New("Content-Type must be application/json")

func requireJSONContentType(r *http.Request) error {
	if len(r.Header.Values("Content-Type")) != 1 {
		return errUnsupportedMediaType
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errUnsupportedMediaType
	}
	return nil
}
func writeJSONBodyError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, errUnsupportedMediaType) {
		status = http.StatusUnsupportedMediaType
	}
	writeError(w, status, err.Error())
}

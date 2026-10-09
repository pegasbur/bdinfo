package main

import (
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// requestSecurity protects state-changing endpoints from cross-origin browser
// requests. Requests without an Origin header (for example curl/health tooling)
// remain supported. All POST endpoints in this application consume JSON.
func requestSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUnsafeMethod(r.Method) {
			if !sameOriginRequest(r) {
				writeJSON(w, http.StatusForbidden, errorResponse{Error: "cross-origin request rejected"})
				return
			}

			if r.Method == http.MethodPost && !isJSONRequest(r) {
				writeJSON(w, http.StatusUnsupportedMediaType, errorResponse{Error: "Content-Type must be application/json"})
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isJSONRequest(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func sameOriginRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}

	originHost := strings.ToLower(parsed.Host)
	for _, host := range requestHosts(r) {
		if strings.EqualFold(originHost, host) {
			return true
		}
	}

	return false
}

func requestHosts(r *http.Request) []string {
	hosts := make([]string, 0, 2)
	if host := strings.TrimSpace(r.Host); host != "" {
		hosts = append(hosts, strings.ToLower(host))
	}

	// Reverse proxies normally replace X-Forwarded-Host with the original host.
	// Accept only the first value, matching common proxy semantics.
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		if comma := strings.IndexByte(forwarded, ','); comma >= 0 {
			forwarded = forwarded[:comma]
		}
		forwarded = strings.TrimSpace(forwarded)
		if forwarded != "" {
			hosts = append(hosts, strings.ToLower(forwarded))
		}
	}

	return hosts
}

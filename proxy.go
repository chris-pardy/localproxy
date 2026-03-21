package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

type ProxyHandler struct {
	registry  *Registry
	logger    *slog.Logger
	dashboard http.Handler
}

func NewProxyHandler(registry *Registry, logger *slog.Logger) *ProxyHandler {
	p := &ProxyHandler{
		registry: registry,
		logger:   logger,
	}
	p.dashboard = http.HandlerFunc(p.serveDashboard)
	return p
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	if host == "localhost" || host == "127.0.0.1" {
		p.dashboard.ServeHTTP(w, r)
		return
	}

	if !strings.HasSuffix(host, ".localhost") {
		http.Error(w, "not a .localhost domain", http.StatusBadRequest)
		return
	}

	name := strings.TrimSuffix(host, ".localhost")
	reg, ok := p.registry.Resolve(name)
	if !ok {
		p.serveNotFound(w, r, name)
		return
	}

	proxy := p.getOrCreateProxy(reg.Port, name)
	p.logger.Debug("proxying", "name", name, "port", reg.Port, "source", reg.Source.String())
	proxy.ServeHTTP(w, r)
}

func (p *ProxyHandler) getOrCreateProxy(port int, name string) *httputil.ReverseProxy {
	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", port),
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Rewrite Host header to what the dev server expects
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalHost := req.Host
		originalDirector(req)
		req.Host = fmt.Sprintf("localhost:%d", port)
		req.Header.Set("X-Forwarded-Host", originalHost)
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		p.serveUpstreamError(w, name, port, err)
	}

	return proxy
}

func (p *ProxyHandler) serveNotFound(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)

	all := p.registry.All()
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>localproxy — not found</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 600px; margin: 60px auto; padding: 0 20px; color: #333; }
h1 { color: #c00; }
a { color: #07c; }
ul { line-height: 2; }
code { background: #f0f0f0; padding: 2px 6px; border-radius: 3px; }
</style></head><body>
<h1>Project %q not found</h1>
<p>No service is registered for <code>%s.localhost</code>.</p>`, name, name)

	if len(all) > 0 {
		fmt.Fprintf(w, "<h2>Available projects</h2><ul>")
		for _, reg := range all {
			fmt.Fprintf(w, `<li><a href="http://%s.localhost">%s</a> (port %d, via %s)</li>`,
				reg.Name, reg.Name, reg.Port, reg.Source)
		}
		fmt.Fprintf(w, "</ul>")
	}

	fmt.Fprintf(w, `<h2>How to register</h2>
<ul>
<li>Start a dev server in a directory under your configured roots</li>
<li>Create a <code>.localhost</code> file with <code>port = NNNN</code></li>
<li>Run <code>localproxy register %s PORT</code></li>
</ul>
</body></html>`, name)
}

func (p *ProxyHandler) serveUpstreamError(w http.ResponseWriter, name string, port int, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)

	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>localproxy — upstream error</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 600px; margin: 60px auto; padding: 0 20px; color: #333; }
h1 { color: #c00; }
code { background: #f0f0f0; padding: 2px 6px; border-radius: 3px; }
</style></head><body>
<h1>Cannot reach %s</h1>
<p>Project <code>%s</code> is registered on port <code>%d</code> but is not responding.</p>
<p>Error: %v</p>
<p>Is your dev server running?</p>
</body></html>`, name, name, port, err)
}

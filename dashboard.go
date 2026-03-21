package main

import (
	"fmt"
	"net/http"
	"sort"
	"time"
)

func (p *ProxyHandler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	all := p.registry.All()
	sort.Slice(all, func(i, j int) bool {
		return all[i].Name < all[j].Name
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html>
<html><head><title>localproxy</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: system-ui, -apple-system, sans-serif; max-width: 800px; margin: 40px auto; padding: 0 20px; color: #1a1a1a; background: #fafafa; }
h1 { font-size: 1.5rem; margin-bottom: 0.25rem; }
.subtitle { color: #666; margin-bottom: 2rem; font-size: 0.9rem; }
table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.08); }
th { text-align: left; padding: 10px 16px; background: #f5f5f5; font-weight: 600; font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.5px; color: #666; }
td { padding: 10px 16px; border-top: 1px solid #eee; }
a { color: #0066cc; text-decoration: none; }
a:hover { text-decoration: underline; }
.status { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 8px; }
.status.up { background: #22c55e; }
.status.down { background: #ef4444; }
.source { font-size: 0.8rem; color: #888; background: #f0f0f0; padding: 2px 8px; border-radius: 4px; }
.empty { text-align: center; padding: 40px; color: #999; }
code { background: #f0f0f0; padding: 2px 6px; border-radius: 3px; font-size: 0.85em; }
.help { margin-top: 2rem; padding: 1rem; background: #fff; border-radius: 8px; box-shadow: 0 1px 3px rgba(0,0,0,0.08); font-size: 0.9rem; line-height: 1.6; }
.help h2 { font-size: 1rem; margin-bottom: 0.5rem; }
.help ul { margin-left: 1.5rem; }
</style>
</head><body>
<h1>localproxy</h1>
<p class="subtitle">Local reverse proxy for *.localhost</p>
`)

	if len(all) == 0 {
		fmt.Fprint(w, `<table><tr><td class="empty">No projects registered. Start a dev server or use <code>localproxy register</code>.</td></tr></table>`)
	} else {
		fmt.Fprint(w, `<table>
<tr><th></th><th>Project</th><th>Port</th><th>Source</th><th>Directory</th></tr>`)
		for _, reg := range all {
			status := "down"
			if isHTTPAlive(reg.Port) {
				status = "up"
			}
			dir := reg.Dir
			if dir == "" {
				dir = "-"
			}
			fmt.Fprintf(w, `<tr>
<td><span class="status %s"></span></td>
<td><a href="http://%s.localhost">%s</a></td>
<td>%d</td>
<td><span class="source">%s</span></td>
<td>%s</td>
</tr>`, status, reg.Name, reg.Name, reg.Port, reg.Source, dir)
		}
		fmt.Fprint(w, `</table>`)
	}

	fmt.Fprint(w, `
<div class="help">
<h2>Quick start</h2>
<ul>
<li>Dev servers under your root directories are auto-detected via process scanning</li>
<li>Docker containers with published ports are auto-detected</li>
<li>Register manually: <code>localproxy register my-app 3000</code></li>
<li>Add a <code>.localhost</code> file to a project directory with <code>port = 3000</code></li>
</ul>
</div>
</body></html>`)
}

var probeClient = &http.Client{
	Timeout: 200 * time.Millisecond,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func isHTTPAlive(port int) bool {
	req, err := http.NewRequest("OPTIONS", fmt.Sprintf("http://localhost:%d/", port), nil)
	if err != nil {
		return false
	}
	resp, err := probeClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true // any HTTP status code means a real server is there
}

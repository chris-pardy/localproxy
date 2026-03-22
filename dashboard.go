package main

import (
	"fmt"
	"net/http"
	"sort"
)

func (p *ProxyHandler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	groups := p.registry.AllGrouped()
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Default.Name < groups[j].Default.Name
	})
	// Sort variants within each group
	for i := range groups {
		sort.Slice(groups[i].Variants, func(a, b int) bool {
			return groups[i].Variants[a].Name < groups[i].Variants[b].Name
		})
	}

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
.source { font-size: 0.8rem; color: #888; background: #f0f0f0; padding: 2px 8px; border-radius: 4px; }
.empty { text-align: center; padding: 40px; color: #999; }
code { background: #f0f0f0; padding: 2px 6px; border-radius: 3px; font-size: 0.85em; }
.help { margin-top: 2rem; padding: 1rem; background: #fff; border-radius: 8px; box-shadow: 0 1px 3px rgba(0,0,0,0.08); font-size: 0.9rem; line-height: 1.6; }
.help h2 { font-size: 1rem; margin-bottom: 0.5rem; }
.help ul { margin-left: 1.5rem; }
.expander { cursor: pointer; user-select: none; font-size: 0.75rem; color: #666; margin-left: 6px; }
.expander:hover { color: #0066cc; }
.variant-row { display: none; }
.variant-row td { padding-left: 32px; background: #fafafa; }
.variant-row td:first-child { padding-left: 16px; }
</style>
</head><body>
<h1>localproxy</h1>
<p class="subtitle">Local reverse proxy for *.localhost</p>
`)

	if len(groups) == 0 {
		fmt.Fprint(w, `<table><tr><td class="empty">No projects registered. Start a dev server or use <code>localproxy register</code>.</td></tr></table>`)
	} else {
		fmt.Fprint(w, `<table>
<tr><th>Project</th><th>Port</th><th>Source</th><th>Directory</th></tr>`)
		for i, g := range groups {
			reg := g.Default
			dir := reg.Dir
			if dir == "" {
				dir = "-"
			}
			expanderHTML := ""
			if len(g.Variants) > 0 {
				expanderHTML = fmt.Sprintf(
					` <span class="expander" onclick="toggleVariants('group-%d', this)">+ %d more</span>`,
					i, len(g.Variants))
			}
			fmt.Fprintf(w, `<tr>
<td><a href="http://%s.localhost">%s</a>%s</td>
<td>%d</td>
<td><span class="source">%s</span></td>
<td>%s</td>
</tr>`, reg.Name, reg.Name, expanderHTML, reg.Port, reg.Source, dir)

			for _, v := range g.Variants {
				fmt.Fprintf(w, `<tr class="variant-row" data-group="group-%d">
<td><a href="http://%s.localhost">%s</a></td>
<td>%d</td>
<td><span class="source">%s</span></td>
<td></td>
</tr>`, i, v.Name, v.Name, v.Port, v.Source)
			}
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
<li>All detected ports are accessible via <code>&lt;project&gt;-&lt;port&gt;.localhost</code></li>
<li>Map named subdomains in <code>.localhost</code>:<br>
<code>[ports]</code><br>
<code>api = 3001</code><br>
<code>docs = 4000</code><br>
creates <code>api.&lt;project&gt;.localhost</code> and <code>docs.&lt;project&gt;.localhost</code></li>
</ul>
</div>
<script>
function toggleVariants(group, el) {
	var rows = document.querySelectorAll('[data-group="' + group + '"]');
	var visible = rows[0] && rows[0].style.display === 'table-row';
	for (var i = 0; i < rows.length; i++) {
		rows[i].style.display = visible ? 'none' : 'table-row';
	}
	var count = rows.length;
	el.textContent = visible ? '+ ' + count + ' more' : '- ' + count + ' more';
}
</script>
</body></html>`)
}


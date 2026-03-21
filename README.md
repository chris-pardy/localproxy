# localproxy

A local reverse proxy that maps `<project>.localhost` to the right port automatically. No more remembering port numbers.

## How it works

localproxy runs on port 80 and routes requests based on the `Host` header. Visit `http://recipe-book.localhost` and it proxies to whatever port your dev server is running on.

`*.localhost` resolves to `127.0.0.1` natively in modern browsers (RFC 6761) — no DNS configuration needed.

### Discovery mechanisms

Projects are discovered automatically through four mechanisms (highest priority first):

1. **Backchannel** — Apps register explicitly via a Unix socket
2. **Dotfile** — A `.localhost` file in the project directory
3. **Docker** — Containers with published ports, mapped via Compose labels
4. **Process scanner** — Scans `lsof` every 3s, matches listening ports to project directories

### Subdomain mapping

Directory structure maps to subdomains in DNS order:

```
~/Code/app              → app.localhost
~/Code/app/service      → service.app.localhost
~/Code/app/pkg/web      → web.pkg.app.localhost
```

When multiple ports are found for a project, localproxy probes with HTTP OPTIONS and picks the one that responds as an HTTP server.

## Install

```bash
go build -o localproxy .
sudo ./localproxy install -roots ~/Code
```

Specify the directories where your projects live with `-roots` (comma-separated):

```bash
sudo ./localproxy install -roots ~/Code,~/Projects,~/Work
```

The `-roots` setting is preserved across reinstalls/updates — you only need to specify it once. Subsequent `sudo ./localproxy install` will remember your previous roots.

This installs a LaunchDaemon that starts on boot. Visit http://localhost for the dashboard.

## Usage

### Automatic detection

Start a dev server anywhere under `~/Code` and it will be detected within 3 seconds.

### Manual registration

```bash
localproxy register my-app 3000
localproxy unregister my-app
localproxy list
```

### Dotfile (`.localhost`)

Add a `.localhost` file to any project directory:

```
# Override the auto-detected name
name = my-app

# Pin to a specific port
port = 3000
```

### Backchannel (Unix socket)

Apps can self-register via the Unix socket at `/var/run/localproxy.sock`:

```bash
echo '{"action":"register","name":"my-app","port":3000}' | nc -U /var/run/localproxy.sock
```

## Configuration

```
localproxy -listen :80 -roots ~/Code,~/Projects -socket /var/run/localproxy.sock -scan-interval 3s
```

Environment variables: `LOCALPROXY_LISTEN`, `LOCALPROXY_ROOTS`, `LOCALPROXY_SOCKET`.

## Uninstall

```bash
sudo localproxy uninstall
```

## License

MIT

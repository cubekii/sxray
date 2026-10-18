# SXRAY

### minimal example
```yaml
xray:
  image: ghcr.io/cubekii/sxray:1.21
  restart: always
  environment:
    XRAY_KEY: ${XRAY_VLESS_KEY}
    XRAY_ROUTE_MODE: http
    XRAY_LISTEN_ADDR: xray
    XRAY_HTTP_PORT: 10809
  expose:
    - "10809"
```

## Usage

SXRAY runs with `xray run` (or just `xray`, it is the default command).
Instead of a config file, pass one or more share links with `-k`, choose a
local inbound with `-m`, and the config is generated in memory:

```bash
xray run -k "${XRAY_VLESS_KEY}" -m http -listen 0.0.0.0
```

```bash
# SOCKS5 proxy on 0.0.0.0:9050 and HTTP proxy on 0.0.0.0:8080
xray run -k "${XRAY_VLESS_KEY}" -m socks -m http -socks-port 9050 -http-port 8080 -listen 0.0.0.0
```

```bash
# Route the whole system through the link (TUN mode)
xray run -k "${XRAY_VLESS_KEY}" -m tun
```

```bash
# Print the generated config without starting the server
xray run -k "${XRAY_VLESS_KEY}" -m http -dump
```

Config files still work as usual:

```bash
xray run -c /etc/xray/config.json
```

## Environment variables

Every run flag has an environment variable equivalent. Flags always take
precedence over environment variables, so a plain `xray` with the variables
set below needs no arguments at all.

| Variable | Flag | Default | Description |
| --- | --- | --- | --- |
| `XRAY_CONFIG_PATH` | `-config`, `-c` | - | Config file path(s), multiple separated by newline |
| `XRAY_KEY` | `-key`, `-k` | - | Share link(s) or subscription(s), multiple separated by newline |
| `XRAY_ROUTE_MODE` | `-mode`, `-m` | - | Local inbound(s): `tun`, `socks`, `http`, separated by newline or comma |
| `XRAY_LISTEN_ADDR` | `-listen` | `127.0.0.1` | Listen address for the socks and http modes |
| `XRAY_SOCKS_PORT` | `-socks-port` | `10808` | Port of the socks mode inbound |
| `XRAY_HTTP_PORT` | `-http-port` | `10809` | Port of the http mode inbound |

```bash
export XRAY_KEY="vless://..."
export XRAY_ROUTE_MODE="http,socks"
export XRAY_LISTEN_ADDR="0.0.0.0"
export XRAY_SOCKS_PORT="9050"
export XRAY_HTTP_PORT="8080"

xray   # same as: xray run -m http -m socks -listen 0.0.0.0 -socks-port 9050 -http-port 8080
```
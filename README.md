# SXRAY

### simple example
```yaml
xray:
  image: ghcr.io/cubekii/sxray:1.21
  restart: always
  environment:
    XRAY_KEY: ${XRAY_VLESS_KEY}
    XRAY_ROUTE_MODE: http
    XRAY_LISTEN_ADDR: xray
  expose:
    - "10809"
```
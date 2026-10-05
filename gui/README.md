# Memcached GUI

A Wails and Svelte desktop tool for inspecting and changing Memcached data.

- Save server contexts and switch between connections.
- Get values with TTL, CAS, flags, size, and access metadata; display text or JSON.
- Set, delete, increment, decrement, inspect stats and version, and flush all keys.
- Review the operation log while working.

## Development

Install Go 1.26+, Node.js/npm, and the [Wails CLI and platform dependencies](https://wails.io/docs/gettingstarted/installation). From the repository root:

```bash
cd gui
npm --prefix frontend install
wails dev
```

To build a desktop binary, run `wails build` from `gui/`. The GUI uses the `memcached-gui` Go module and the client in this repository through `go.work` during local development.

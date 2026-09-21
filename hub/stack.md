# Stack

| Concern | Choice |
| --- | --- |
| Language | Go, one static cgo-free binary; `go:embed` carries the panel |
| State | SQLite via `modernc.org/sqlite` — [store](internal/store/README.md) |
| SSH | `golang.org/x/crypto/ssh`, host keys pinned — [remote](internal/remote/README.md) |
| Peer network | `go-libp2p` on a Kademlia DHT, the public one or a private one of hubs — [peers](internal/peers/README.md) |
| Inference router | `net/http` streaming proxy in Ollama's shape — [pool](internal/pool/README.md) |
| Scheduler | `robfig/cron/v3` in-process — [tasks](internal/tasks/README.md) |
| Sign-in | `go-webauthn/webauthn` passkeys, two roles — [auth](internal/auth/README.md) |
| Session tools | `modelcontextprotocol/go-sdk` at `/api/mcp` — [mcp](internal/mcp/README.md) |
| CLI | `net/http` client on the same API; `flag` for parsing — [cli](internal/cli/README.md) |
| Agent | [oh-my-pi](https://github.com/can1357/oh-my-pi) `omp`, pinned — [agent](internal/agent/README.md) |
| Model fit | [llmfit](https://github.com/AlexsJones/llmfit), pinned — [fleet](internal/fleet/README.md) |
| Storage clusters | mergerfs over NFS on the remotes — [storage](internal/storage/README.md) |
| Service fronts | `net/http` reverse proxy; `acme/autocert` for an ingress — [server](internal/server/README.md) |
| Export encryption | `filippo.io/age` (passphrase) — [backup](internal/backup/README.md) |
| Panel | Svelte 5 + Vite, static, embedded — [ui](ui/README.md) |
| Packaging | nfpm → `.deb` for `amd64` and `arm64` — [packaging](packaging/README.md) |

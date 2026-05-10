# kite-mcp-riskguard

[![Go Reference](https://pkg.go.dev/badge/github.com/algo2go/kite-mcp-riskguard.svg)](https://pkg.go.dev/github.com/algo2go/kite-mcp-riskguard)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Pre-trade risk safety controls for the algo2go ecosystem. 8+ checks
gate every order before it reaches the broker: kill switch,
order-value cap, qty cap, daily count limit, per-second/per-minute
rate limit, duplicate detection, daily-value cap, auto-freeze circuit
breaker, OTR (Order-to-Trade Ratio) band check, market-hours guard,
margin check, and plugin-discovery subprocess checks for custom
extensions.

Used by [`Sundeepg98/kite-mcp-server`](https://github.com/Sundeepg98/kite-mcp-server)
as the gate between Audit and Elicitation in the order execution
chain: Audit -> Riskguard -> Elicitation -> Kite API.

## Why a separate module?

Risk gating is a foundational safety primitive that any algo2go
consumer placing orders should use independently of
`kite-mcp-server`'s broker integration. Hosting as its own module:

- Centralizes the Guard contract + 8+ check implementations
- Lets check thresholds + rate-limit policies version independently
- Decouples plugin-discovery subprocess hooks from any one runtime
- Provides a stable RPC contract (`checkrpc/`) for external check
  plugins written in any language

## Stability promise

**v0.x — unstable.** Pin `v0.1.0` deliberately.

## Install

```bash
go get github.com/algo2go/kite-mcp-riskguard@v0.1.0
```

## Public API

- `Guard` — orchestrates all checks; main entry point
- `KillSwitch`, `CircuitLimit`, `Limits`, `Trackers`, `PerSecond`,
  `Dedup`, `MarginCheck`, `MarketHours`, `OTRBand` — individual
  checks
- `SubprocessCheck` — plugin-discovery + RPC for external check
  plugins
- `Middleware` — Guard wrapped as a middleware for use-case chains
- `checkrpc/` — RPC types for external plugins (zero algo2go deps,
  embeddable in any language binding)

## Dependencies

- `github.com/algo2go/kite-mcp-alerts` v0.1.0
- `github.com/algo2go/kite-mcp-domain` v0.1.0
- `github.com/algo2go/kite-mcp-i18n` v0.1.0
- `github.com/algo2go/kite-mcp-logger` v0.1.0
- `github.com/algo2go/kite-mcp-oauth` v0.1.0
- `github.com/hashicorp/go-plugin` — RPC plugin framework
- `github.com/stretchr/testify` v1.10.0

All algo2go deps published; no upstream `replace` directives needed.

## Reference consumer

[`Sundeepg98/kite-mcp-server`](https://github.com/Sundeepg98/kite-mcp-server)
— consumed across 53 .go files: kc/manager_*, kc/options.go,
kc/config.go, kc/broker_services.go, kc/ports/order.go,
kc/papertrading/*, kc/usecases/*, kc/telegram/*, kc/ops/*, app/*,
mcp/admin/*, mcp/middleware/*, mcp/common/*, mcp/misc/*,
mcp/tools_*_test.go, examples/riskguard-check-plugin/main.go.

## License

MIT — see [LICENSE](LICENSE).

## Authors

Original design: [Sundeepg98](https://github.com/Sundeepg98) (Zerodha
Tech). Multi-module promotion (2026-05-10): algo2go contributors.

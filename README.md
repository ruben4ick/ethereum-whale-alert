# Ethereum Whale Alert

Real-time monitoring tool that connects to an Ethereum node via WebSocket, detects large ("whale") transactions, and sends alerts to Slack and Discord.

## Features

- Real-time new block subscription via WebSocket (go-ethereum)
- Whale transaction detection by configurable ETH threshold
- ERC-20 token transfer monitoring
- Slack & Discord webhook notifications
- Prometheus metrics (`/metrics` endpoint)
- Grafana dashboard for visualization

## Architecture

```
Ethereum Node (WebSocket)
        │
        ▼
   EthereumClient (go-ethereum ethclient)
        │
        ▼
   Watcher (subscribe blocks → filter transactions → detect whales)
        │
        ▼
   Notifier (fan-out)
    ├── Slack Webhook
    └── Discord Webhook
```
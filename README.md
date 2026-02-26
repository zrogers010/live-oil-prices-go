# Live Oil Prices

Real-time energy market intelligence platform with AI-powered analysis, interactive charts, and institutional-grade market data.

## Tech Stack

- **Backend**: Go (standard library `net/http`, Go 1.23+)
- **Frontend**: TypeScript, [Lightweight Charts](https://github.com/nicolo-ribaudo/lightweight-charts) (TradingView)
- **Build**: esbuild for TypeScript bundling

## Quick Start

```bash
# Install dependencies and build everything
make build

# Start the server
make run
```

The server starts at **http://localhost:8080**.

## Development

```bash
# Watch mode — rebuilds TypeScript on change + runs Go server
make dev
```

## Project Structure

```
├── cmd/server/main.go          # Server entry point
├── internal/
│   ├── handlers/handlers.go    # API route handlers
│   ├── middleware/middleware.go # HTTP middleware (CORS, logging, recovery)
│   ├── models/models.go        # Data structures
│   └── services/market_data.go # Market data generation
├── web/
│   ├── src/                    # TypeScript source
│   │   ├── app.ts              # Main application
│   │   ├── api.ts              # API client
│   │   ├── charts.ts           # Chart initialization
│   │   └── types.ts            # Type definitions
│   └── static/                 # Served static files
│       ├── index.html
│       ├── css/styles.css
│       └── js/app.js           # Built output
├── go.mod
├── package.json
├── tsconfig.json
└── Makefile
```

## API Endpoints

| Endpoint | Description |
|---|---|
| `GET /api/prices` | Current prices for all tracked commodities |
| `GET /api/charts/{symbol}?days=90` | OHLCV chart data |
| `GET /api/news` | Energy market news feed |
| `GET /api/predictions` | AI price predictions |
| `GET /api/analysis` | Market analysis with technical signals |
| `GET /api/health` | Health check |

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Server port |

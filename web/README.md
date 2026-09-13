# BDInfo frontend

React + TypeScript + Vite frontend for the BDInfo web application.

## Development

```bash
npm ci
npm run dev
```

## Checks

```bash
npm run build
npm run lint
```

Vite copies `public/` into the frontend build. The Docker build then copies the generated `dist/` directory into `cmd/server/ui`, where the Go server embeds it into the final binary.

# Stellabill Backend

Go (Gin) API backend for Stellabill — subscription and billing plans API. This repo is backend-only; a separate frontend consumes these APIs.

---

## Table of contents

- [Tech stack](#tech-stack)
- [What this backend provides (for the frontend)](#what-this-backend-provides-for-the-frontend)
- [Local setup](#local-setup)
- [Configuration](#configuration)
- [API reference](#api-reference)
- [Contributing (open source)](#contributing-open-source)
- [Project layout](#project-layout)
- [License](#license)

---

## Tech stack

- **Language:** Go 1.22+
- **Framework:** [Gin](https://github.com/gin-gonic/gin)
- **Config:** Environment variables (no config files required for default dev)

---

## What this backend provides (for the frontend)

This service is the **backend only**. A separate frontend (or any client) can:

- **Health check** — `GET /api/health` to verify the API is up.
- **Plans** — `GET /api/plans` to list billing plans (id, name, amount, currency, interval, description). Currently returns an empty list; DB integration is planned.
- **Subscriptions** — `GET /api/subscriptions` to list subscriptions and `GET /api/subscriptions/:id` to fetch one. Responses include plan_id, customer, status, amount, interval, next_billing. Currently placeholder/mock data; DB integration is planned.

CORS is enabled for all origins in development so a frontend on another port or domain can call these endpoints.

---

## Local setup

### Prerequisites

- **Go 1.22 or later**  
  - Check: `go version`  
  - Install: [https://go.dev/doc/install](https://go.dev/doc/install)

- **Git** (for cloning and contributing)

- **PostgreSQL** (optional for now; app runs without it using default config; DB will be used when persistence is added)

### 1. Clone the repository

```bash
git clone https://github.com/YOUR_ORG/stellabill-backend.git
cd stellabill-backend
```

### 2. Install dependencies

```bash
go mod download
```

### 3. (Optional) Environment variables

Create a `.env` file in the project root (do not commit it; it’s in `.gitignore`):

```bash
# Optional — defaults shown
ENV=development
PORT=8080
DATABASE_URL=postgres://localhost/stellarbill?sslmode=disable
JWT_SECRET=change-me-in-production
```

Or export them in your shell. The app will run with the defaults if you don’t set anything.

### 4. Run the server

```bash
go run ./cmd/server
```

Server listens on `http://localhost:8080` (or the port you set via `PORT`).

### 5. Verify

```bash
curl http://localhost:8080/api/health
# Expected: {"service":"stellarbill-backend","status":"ok"}
```

---

## Configuration

| Variable        | Default                                      | Description                    |
|----------------|----------------------------------------------|--------------------------------|
| `ENV`          | `development`                                | Environment (e.g. production)  |
| `PORT`         | `8080`                                       | HTTP server port               |
| `DATABASE_URL` | `postgres://localhost/stellarbill?sslmode=disable` | PostgreSQL connection string   |
| `JWT_SECRET`   | `change-me-in-production`                     | Secret for JWT (change in prod)|

In production, set these via your host’s environment or secrets manager; do not commit secrets.

---

## API reference

Base URL (local): `http://localhost:8080`

| Method | Path                     | Description              |
|--------|--------------------------|--------------------------|
| GET    | `/api/health`            | Health check             |
| GET    | `/api/plans`             | List billing plans       |
| GET    | `/api/subscriptions`     | List subscriptions       |
| GET    | `/api/subscriptions/:id` | Get one subscription     |

All JSON responses. CORS allowed for `*` origin with common methods and headers.

---

## Contributing (open source)

We welcome contributions from the community. Below is a short guide to get you from “first look” to “merged change”.

### Code of conduct

- Be respectful and inclusive.
- Focus on constructive feedback and clear, factual communication.

### How to contribute

1. **Open an issue**  
   - Bug: describe what you did, what you expected, and what happened.  
   - Feature: describe the goal and why it helps.

2. **Fork and clone**  
   - Fork the repo on GitHub, then clone your fork locally.

3. **Create a branch**  
   ```bash
   git checkout -b fix/your-fix   # or feature/your-feature
   ```

4. **Make changes**  
   - Follow existing style (format with `go fmt`).  
   - Keep commits logical and messages clear (e.g. “Add validation for plan ID”).

5. **Run checks**  
   ```bash
   go build ./...
   go vet ./...
   go fmt ./...
   ```  
   Add or run tests if the project has them.

6. **Commit**  
   - Prefer small, atomic commits (one logical change per commit).

7. **Push and open a PR**  
   ```bash
   git push origin fix/your-fix
   ```  
   - Open a Pull Request against the main branch.  
   - Fill in the PR template (if any).  
   - Link related issues.  
   - Describe what you changed and why.

8. **Review**  
   - Address review comments. Maintainers will merge when everything looks good.

### Development workflow

- Use the [Local setup](#local-setup) steps to run the server.
- Change code, restart the server (or use a tool like `air` for live reload if the project adds it).
- Test with `curl` or the frontend that consumes this API.

### Project standards

- **Go:** `go fmt`, `go vet`, no unnecessary dependencies.  
- **APIs:** Keep JSON shape stable; document breaking changes in PRs.  
- **Secrets:** Never commit `.env`, keys, or passwords.

---

## Project layout

```
stellabill-backend/
├── cmd/
│   └── server/
│       └── main.go          # Entry point, Gin router, server start
├── internal/
│   ├── config/
│   │   └── config.go        # Loads ENV, PORT, DATABASE_URL, JWT_SECRET
│   ├── handlers/
│   │   ├── health.go        # GET /api/health
│   │   ├── plans.go         # GET /api/plans
│   │   └── subscriptions.go # GET /api/subscriptions, /api/subscriptions/:id
│   └── routes/
│       └── routes.go        # Registers routes and CORS middleware
├── go.mod
├── go.sum
├── .gitignore
└── README.md
```

---

## License

See the LICENSE file in the repository (if present). If none, assume proprietary until stated otherwise.

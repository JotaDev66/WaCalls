# Contributing to WaCalls

Thanks for your interest in improving WaCalls. This guide covers the local setup,
the quality gates every change must pass, and the pull request flow.

## Dev setup

Requirements: Go 1.26+, Node 22+, and optionally Docker for the compose stack.

```bash
cp .env.example .env
make dev
```

`make dev` runs the Go server under [air](https://github.com/air-verse/air) on `:3001`
(rebuild and restart on `.go` changes) and the Vite client on `:5173`, which proxies
`/api` to the server. Open http://localhost:5173.

## Tests and lint

CI blocks on all of these, so run them before pushing:

```bash
make test    # go test ./... -count=1
make lint    # golangci-lint v2.12.2 (pinned in CI), zero issues required
```

For the web client:

```bash
cd client
npx tsc -b           # type-check
npm run lint         # eslint, zero warnings required
npm run format:check # prettier
npm run build
```

CI also blocks on govulncheck (Go vulnerability scan) for every push and pull request.

CI also runs the race detector (`CGO_ENABLED=1 go test -race ./internal/voip/...`).
Running it locally requires a C toolchain; on machines without one, use a Go Docker
image:

```bash
docker run --rm -v "$(pwd):/src" -w /src golang:1.26 go test -race ./internal/voip/...
```

## Workflow

1. Fork the repository and create a branch from `develop`: `feat/*`, `fix/*`, or `chore/*`.
2. Open the pull request against `develop`. `main` is the release branch and is not a
   valid PR target.
3. Behavior changes need a test. Keep pull requests small and focused on one thing.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): subject`
with types like `feat`, `fix`, `refactor`, `chore`, `test`, `docs`. Subjects are
imperative English, lowercase start, no emoji, no trailing period.

## Scope

WaCalls is audio-only by design. Video calling was evaluated and removed from the
roadmap, so pull requests adding video support will not be accepted.

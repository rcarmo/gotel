---
name: Go project conventions
description: Project conventions with module caching, linting, security checks, and tests via Make
---

# Skill: Go project conventions

## Goal
Provide a standard Go workflow with module caching, linting, security checks, and tests driven by Make.

## Make targets (recommended)
- `make deps` → `go mod download` + `go mod tidy`
- `make vet` → `go vet ./...`
- `make lint` → `fmt` + `vet` + `staticcheck` (if available)
- `make security` → `gosec ./...` (if available)
- `make test` → `go test -v ./...`
- `make check` → `make lint && make test` (standard validation pipeline)

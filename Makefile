KEYCLOAK_IMAGE ?= quay.io/keycloak/keycloak:26.4.0
KEYCLOAK_NAME ?= mcpskills-keycloak
KEYCLOAK_PORT ?= 8180
CONTAINER_ENGINE ?= $(shell if command -v podman >/dev/null 2>&1 && podman info >/dev/null 2>&1; then command -v podman; elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then command -v docker; fi)
KEYCLOAK_URL = http://localhost:$(KEYCLOAK_PORT)
KEYCLOAK_DISCOVERY = $(KEYCLOAK_URL)/realms/mcpskills-local/.well-known/openid-configuration
OTEL_VIEWER = $(abspath _bin/otel-desktop-viewer)

.PHONY: keycloak-up keycloak-down keycloak-logs keycloak-ready otel-install demo-otel

otel-install: _bin/otel-desktop-viewer

_bin:
	mkdir -p "$@"

_bin/otel-desktop-viewer: | _bin
	GOBIN="$(abspath _bin)" go install github.com/CtrlSpice/otel-desktop-viewer@latest

demo-otel: otel-install
	@"$(OTEL_VIEWER)" --open-browser=false & \
		viewer_pid=$$!; \
		trap 'kill $$viewer_pid 2>/dev/null || true; wait $$viewer_pid 2>/dev/null || true' EXIT; \
		attempt=0; until curl --fail --silent --output /dev/null http://localhost:8000/; do \
			if ! kill -0 $$viewer_pid 2>/dev/null; then printf '%s\n' 'OTel Desktop Viewer exited before becoming ready.' >&2; exit 1; fi; \
			attempt=$$((attempt + 1)); \
			if [ "$$attempt" -ge 30 ]; then printf '%s\n' 'OTel Desktop Viewer did not become ready.' >&2; exit 1; fi; \
			sleep 1; \
		done; \
		printf 'OTel traces: %s\nOTel metrics: %s\nMCP server: %s\n' 'http://localhost:8000/traces' 'http://localhost:8000/metrics' "$${MCP_RESOURCE_URL:-http://localhost:8080/mcp}"; \
		MCP_SKILLS_DIR=docs/examples/skills MCP_POLICY_FILE=docs/examples/skill-policy.yaml \
			OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run ./cmd/mcpskills

keycloak-up:
	@test -n "$(CONTAINER_ENGINE)" || { printf '%s\n' 'Start Podman or Docker, or set CONTAINER_ENGINE.' >&2; exit 1; }
	@if $(CONTAINER_ENGINE) container inspect "$(KEYCLOAK_NAME)" >/dev/null 2>&1; then \
		printf '%s\n' 'Keycloak container already exists; run make keycloak-down to recreate it from JSON.'; \
	else \
		$(CONTAINER_ENGINE) run --rm -d --name "$(KEYCLOAK_NAME)" \
			-p "127.0.0.1:$(KEYCLOAK_PORT):8080" \
			-e KC_BOOTSTRAP_ADMIN_USERNAME=admin -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin \
			-v "$(abspath local/keycloak):/opt/keycloak/data/import:ro" \
			"$(KEYCLOAK_IMAGE)" start-dev --hostname="$(KEYCLOAK_URL)" --import-realm || exit $$?; \
	fi
	@$(MAKE) --no-print-directory keycloak-ready

keycloak-ready:
	@attempt=0; until curl --fail --silent --output /dev/null "$(KEYCLOAK_DISCOVERY)"; do \
		attempt=$$((attempt + 1)); \
		if [ "$$attempt" -ge 60 ]; then printf '%s\n' 'Keycloak did not become ready; run make keycloak-logs.' >&2; exit 1; fi; \
		sleep 2; \
	done
	@printf 'Keycloak ready at %s\n' "$(KEYCLOAK_URL)"

keycloak-down:
	@test -n "$(CONTAINER_ENGINE)" || { printf '%s\n' 'Start Podman or Docker, or set CONTAINER_ENGINE.' >&2; exit 1; }
	@$(CONTAINER_ENGINE) rm -f "$(KEYCLOAK_NAME)" 2>/dev/null || true

keycloak-logs:
	@test -n "$(CONTAINER_ENGINE)" || { printf '%s\n' 'Start Podman or Docker, or set CONTAINER_ENGINE.' >&2; exit 1; }
	@$(CONTAINER_ENGINE) logs "$(KEYCLOAK_NAME)"

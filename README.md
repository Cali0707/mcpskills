# MCP Skills prototype

See [configuration](docs/configuration.md) for all server environment variables, policy syntax, and OAuth setup.
For a ready-to-run Keycloak demo with separate engineering and sales skills, see [the example catalog](docs/examples/README.md).

## Local Keycloak

Start a fresh local authorization server with `make keycloak-up` and stop/remove it with `make keycloak-down`. The Makefile detects a running Podman or Docker engine; override the choice with `CONTAINER_ENGINE=docker make keycloak-up` (or `podman`). `make keycloak-logs` displays startup diagnostics. The image is pinned to `quay.io/keycloak/keycloak:26.4.0` and imports [`local/keycloak/mcpskills-local-realm.json`](local/keycloak/mcpskills-local-realm.json). The container binds only to the local host and uses an ephemeral database; stop and start it again to apply changes to the JSON.

| Local setting | Value |
| --- | --- |
| Admin console | http://localhost:8180/admin/ (`admin` / `admin`) |
| Issuer | `http://localhost:8180/realms/mcpskills-local` |
| Discovery | `http://localhost:8180/realms/mcpskills-local/.well-known/openid-configuration` |
| MCP resource audience | `http://localhost:8080/mcp` |
| Access-token entitlement claim | `groups` (group names without leading `/`) |
| Default client scopes | `profile`, `email`, `mcp:skills` |

The realm includes `alice` (`engineering`), `bob` (`sales`), and `carol` (no group), all with the local-only password `dev-password`. The preconfigured public `mcpskills-local-client` supports browser login and direct grants for local testing.

The discovery document advertises Keycloak's OpenID Connect dynamic client registration endpoint. Anonymous DCR is enabled for clients with **localhost or 127.0.0.1 redirect URIs**; the container's published port is bound to `127.0.0.1`. DCR clients inherit the default client scopes, including the `mcp:skills` audience and groups mappers, even when they do not explicitly request a scope. Keycloak's registration response `scope` field can list optional scopes without listing these inherited defaults; check the issued **access token** when diagnosing scopes. The local realm does not require an extra consent prompt for anonymously registered clients.

## Run the protected server

With Keycloak running, the repository's sample policy protects the nested fixture skill for `engineering` while leaving the parent skill available to any authenticated user:

```sh
MCP_SKILLS_DIR=internal/catalog/testdata/skills MCP_POLICY_FILE=local/skill-policy.yaml go run ./cmd/mcpskills
```

The required `MCP_POLICY_FILE` is a separately managed YAML file **outside** `MCP_SKILLS_DIR`. Its keys are full `SKILL.md` URIs; `anyGroup` grants access when the caller has at least one listed Keycloak group. Skills without an entry are available to any authenticated caller. Unknown skill URIs and empty rules cause startup to fail. A policy change takes effect on restart (along with the new catalog snapshot).

`MCP_OAUTH_ISSUER` defaults to `http://localhost:8180/realms/mcpskills-local`; `MCP_RESOURCE_URL` defaults to `http://localhost:8080/mcp`; `MCP_GROUPS_CLAIM` defaults to `groups`. Set `MCP_RESOURCE_URL` to the externally visible MCP URL in deployments, and set `MCP_ENDPOINT_PATH` to its path if different from `/mcp`. Tokens must have this URL in their `aud` claim. The server advertises Keycloak at `/.well-known/oauth-protected-resource/mcp` and challenges unauthenticated requests to `/mcp` with `401` and `WWW-Authenticate`.

The client authenticates through Keycloak and sends a bearer **access token on every HTTP request**. The server validates its signature, issuer, audience and expiry before passing requests to MCP. It filters `skills/list`, `resources/list`, and directory listings, and enforces the same policy on direct skill and file reads. Nested skills have separate ownership: parent manifests cannot grant access to their files. Use HTTPS for issuer and resource URLs outside localhost.

To export attributed usage spans, set `OTEL_EXPORTER_OTLP_ENDPOINT` (or `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`) for an OTLP/HTTP collector; with neither set, tracing is disabled. Spans include validated `user.id` (`iss|sub`), client ID if available, operation, access outcome, resource URI/digest and catalog revision; token and resource contents are excluded.

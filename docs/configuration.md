# Configuration

The server reads configuration from environment variables at startup. Start the local Keycloak realm with `make keycloak-up`, then run the sample skills catalog and policy from the repository root:

```sh
MCP_SKILLS_DIR=internal/catalog/testdata/skills MCP_POLICY_FILE=local/skill-policy.yaml go run ./cmd/mcpskills
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `MCP_LISTEN_ADDR` | `127.0.0.1:8080` | Address on which the HTTP server listens. |
| `MCP_ENDPOINT_PATH` | `/mcp` | Exact Streamable HTTP MCP endpoint path; must be an absolute, clean, non-root path with no trailing slash. |
| `MCP_SKILLS_DIR` | **Required** | Directory containing skills (can be a directory in an existing Git checkout). Loaded as an immutable snapshot on startup. |
| `MCP_POLICY_FILE` | **Required** | Path to the separate YAML authorization policy. Must be outside `MCP_SKILLS_DIR`. |
| `MCP_OAUTH_ISSUER` | `http://localhost:8180/realms/mcpskills-local` | Exact expected Keycloak issuer (`iss`) and OIDC discovery URL base. The server discovers the issuer's JWKS for verifying access tokens. |
| `MCP_RESOURCE_URL` | `http://localhost:8080/mcp` | Externally visible canonical MCP URL. Must match the token's `aud` value and the `resource` in Protected Resource Metadata; its path must match `MCP_ENDPOINT_PATH`. Set this explicitly behind a reverse proxy. |
| `MCP_GROUPS_CLAIM` | `groups` | Name of the top-level access-token claim containing a JSON array of group names. Missing claim means no group entitlements. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Unset | OTLP/HTTP collector base URL for usage spans; tracing is disabled unless this or the traces-specific endpoint below is set. |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | Unset | OTLP/HTTP URL for traces specifically (typically ending in `/v1/traces`). |

The issuer and resource URLs must use HTTPS, except that HTTP is allowed for `localhost` and `127.0.0.1` in local development. The resource URL describes the public endpoint, which can differ from `MCP_LISTEN_ADDR`. If the endpoint path changes, set both `MCP_ENDPOINT_PATH` and `MCP_RESOURCE_URL` consistently. The Keycloak audience mapper must issue access tokens containing the same resource URL in `aud`; the local realm supplies `http://localhost:8080/mcp` by default, so a different URL also requires updating that mapper.

## Skill policy

The policy file keys are the exact `SKILL.md` URIs returned by the loaded catalog:

```yaml
skills:
  "skill://toolbox/nested-tool/SKILL.md":
    anyGroup: [engineering, sales]
```

Authentication is required for every MCP request. A skill not listed in the policy is available to **any authenticated user**. A listed skill requires at least one of its `anyGroup` names in the validated token's configured group claim. An empty `anyGroup`, unknown skill URI, or unknown policy field fails startup. Use `skills: {}` for a catalog without restrictions. The rule protects the skill entry, its `SKILL.md`, its supporting files and directories. Nested skills have separate ownership and can have separate rules; their contents do not inherit access from a parent skill. Restart the server to publish changed policy or skills.

The local realm (`local/keycloak/mcpskills-local-realm.json`) gives `alice` the `engineering` group, `bob` the `sales` group, and `carol` no group. Its default `mcp:skills` client scope installs the MCP audience and groups mappers. `mcp:skills` is a client scope for obtaining an appropriate token; per-skill authorization uses groups, **not** per-skill OAuth scopes. The access token must be sent as `Authorization: Bearer <token>` on every HTTP request. Invalid or missing tokens receive HTTP 401; unknown or inaccessible skill and file URIs return the same MCP not-found error. List responses are filtered for the caller and use private caching.

The resource metadata endpoint is public at `/.well-known/oauth-protected-resource` followed by `MCP_ENDPOINT_PATH` (by default `/.well-known/oauth-protected-resource/mcp`). It advertises the issuer and canonical resource URL so MCP clients can discover and complete OAuth. The `401` response on the MCP endpoint also links to this metadata through `WWW-Authenticate`.

## Usage spans

When either OTLP endpoint variable is set, the server exports OpenTelemetry spans over OTLP/HTTP. The exporter also honors its standard OTLP environment settings, such as `OTEL_EXPORTER_OTLP_HEADERS` for collector authentication. Spans contain the operation, validated `user.id` (`iss|sub`), client ID when available, allow/deny outcome, source revision, and requested URI or digest when applicable. They do not include access tokens or skill content. A read span records a request for content, not its activation in a model's context.

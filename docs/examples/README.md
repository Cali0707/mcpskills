# Keycloak + per-skill access demo

This catalog tells a fictional **Orbit 1.4 launch-day** story. It uses the
existing local Keycloak realm and demonstrates discovery, supporting-file reads,
separate group policies, and a restricted skill nested under a public one.
All product facts and operational scenarios here are fictional.

## Run it

From the repository root, start Keycloak (Podman or Docker) and then start the
server in another terminal:

```sh
make keycloak-up
```

```sh
MCP_SKILLS_DIR=docs/examples/skills MCP_POLICY_FILE=docs/examples/skill-policy.yaml go run ./cmd/mcpskills
```

To see attributed usage traces and file-read counts locally instead, run `make demo-otel` with
Keycloak running. This installs [OTel Desktop Viewer](https://github.com/CtrlSpice/otel-desktop-viewer)
into the gitignored `_bin/` directory if needed (or use `make otel-install` to
install it separately) and starts the viewer and this demo server together; open
http://localhost:8000/traces to see request spans. Read a skill's `SKILL.md` or
supporting file, then open http://localhost:8000/metrics to see the
`mcpskills.resource.reads` counter (periodic export may take up to a minute).
It exports both signals over OTLP/HTTP to `http://localhost:4318`. Stop both
processes with Ctrl-C. Building the viewer requires Go with CGO enabled and a
C compiler.

The server uses the existing defaults: Keycloak issuer
`http://localhost:8180/realms/mcpskills-local` and MCP resource URL
`http://localhost:8080/mcp`. Its OAuth metadata is available at
`http://localhost:8080/.well-known/oauth-protected-resource/mcp`.
Connect your Skills-capable MCP client (such as the local pi build) to
`http://localhost:8080/mcp` using its browser OAuth flow and sign in as one of
the users below. The realm's `mcpskills-local-client` also supports direct
grants for the command-line checks further down.

| Keycloak login | Group | Skills visible via `skills/list` |
| --- | --- | --- |
| `alice` / `dev-password` | `engineering` | `launch-brief`, `engineering-rollout`, `incident-triage` |
| `bob` / `dev-password` | `sales` | `launch-brief`, `customer-update` |
| `carol` / `dev-password` | none | `launch-brief` |

The policy is [`skill-policy.yaml`](skill-policy.yaml), **outside** the served
[`skills/`](skills/) directory. The `launch-brief` skill has no policy entry,
so any authenticated user can read it. Its `engineering-rollout` child is a
different skill with an `engineering` rule. Its `SKILL.md` and supporting
runbook are neither in the parent's manifest nor available by guessing a URI.
The `metadata.audience` frontmatter fields are descriptive labels; authorization
comes only from the separate policy and Keycloak's `groups` token claim.

## Try these prompts

1. Sign in as **carol** and ask: “Give me a one-minute Orbit 1.4 launch
   brief.” The client should discover `launch-brief` and load its
   `references/product-facts.md` when needed. Engineering and sales skills
   should not appear.
2. Sign in as **alice** and ask: “Draft the engineering rollout checklist for
   Orbit 1.4.” The nested `engineering-rollout` skill and its
   `runbooks/rollout-checks.md` are available, as is `incident-triage`.
3. Sign in as **bob** and ask: “Draft a customer email introducing Orbit 1.4.”
   `customer-update` and `references/customer-faq.md` are available, but the
   engineering skills are not.

## Check access without a Skills client

The preconfigured public Keycloak client permits password grants **only for
local demo testing**. With Keycloak and the MCP server running, use `curl` and
`jq` to request a token. Repeat with `DEMO_USER=alice`, `bob`, or `carol` to
compare the results. The access token stays in a shell variable:

```sh
DEMO_USER=alice
TOKEN=$(curl -fsS -X POST \
  http://localhost:8180/realms/mcpskills-local/protocol/openid-connect/token \
  --data-urlencode 'client_id=mcpskills-local-client' \
  --data-urlencode 'grant_type=password' \
  --data-urlencode "username=$DEMO_USER" \
  --data-urlencode 'password=dev-password' \
  --data-urlencode 'resource=http://localhost:8080/mcp' | jq -er '.access_token')
```

Call the real 2026-07-28 Skills method. The current Go SDK sends the result as
a `data:` line in a server-sent event, so the `jq` expression extracts that
line before parsing JSON:

```sh
curl -fsS http://localhost:8080/mcp \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'Mcp-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: skills/list' \
  --data-binary '{"jsonrpc":"2.0","id":1,"method":"skills/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"demo-curl","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}' \
  | jq -Rr 'select(startswith("data: ")) | ltrimstr("data: ") | fromjson | .result.skills[].uri'
```

For **alice**, this lists three skill URIs. For **bob**, it lists two; for
**carol**, one. Try a direct lookup while signed in as bob or carol to show
that hiding a skill from a listing is not the only access check:

```sh
curl -fsS http://localhost:8080/mcp \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'Mcp-Protocol-Version: 2026-07-28' \
  -H 'Mcp-Method: skills/get' \
  --data-binary '{"jsonrpc":"2.0","id":2,"method":"skills/get","params":{"uri":"skill://launch-brief/engineering-rollout/SKILL.md","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"demo-curl","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}' \
  | jq -Rr 'select(startswith("data: ")) | ltrimstr("data: ") | fromjson | .error.code'
```

The inaccessible skill returns MCP not-found code `-32602`. Change
`DEMO_USER`, fetch a new token, and repeat the call as alice to retrieve its
entry. The same skill policy also covers `resources/read`,
`resources/directory/read`, and the standard `resources/list` method.

To reset the ephemeral Keycloak realm, run `make keycloak-down` and then
`make keycloak-up`. Stop the MCP server with Ctrl-C.

# MCP Skills

Distribute skills over HTTP to your internal organization or customers, protected by OAuth. This MCP server serves a skills catalog with per-skill access policies, so signed-in users discover and read only the skills they're allowed to use.

## Get started

- Run `make keycloak-up` for local auth, then follow the [example catalog](docs/examples/README.md) to start the server and try skill access. Use `make keycloak-down` to stop the local realm.
- See [configuration](docs/configuration.md) for server settings, OAuth, and skill policies.
- Run `make demo-otel` to try the example with usage traces.

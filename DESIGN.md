# Skills over MCP Server — Design

## Purpose

Build a prototype Go MCP server that makes Agent Skills available over HTTP from a directory of skills, including a directory inside an already-checked-out Git repository. The server will require OAuth authentication, restrict access to skills using a separate policy file, and record user-attributed usage events.

The prototype should implement the finalized MCP Skills extension rather than inventing a parallel skills API. Its internal boundaries should leave room for Git-managed refresh and, later, a hosted database-backed service.

## Background and protocol

An Agent Skill is a directory containing `SKILL.md` and optional supporting files. The official Skills extension (`io.modelcontextprotocol/skills`, SEP-2640) exposes:

- `skills/list` to enumerate skill entries.
- `skills/get` to retrieve an entry by its `SKILL.md` URI.
- `resources/read` to read `SKILL.md` and supporting files.
- Optional `resources/directory/read` to list a directory's direct children.

Skill entries include the complete frontmatter and a complete manifest of the skill's files, with a SHA-256 digest and byte size for each file. Relative references resolve from the skill's root directory. The server must serve the bytes described by the manifest and prevent paths from escaping that directory. The prototype applies access at the whole-skill level, so every file in a skill inherits the skill's access policy.

The protocol specification is the authority for URI structure, frontmatter, manifests, pagination, caching metadata, and errors:

- [Skills extension overview](https://modelcontextprotocol.io/extensions/skills/overview)
- [SEP-2640](https://modelcontextprotocol.io/seps/2640-skills-extension)
- [MCP specification, 2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28)

## Decisions

### Language, SDK, and client

- Implement the first version in **Go**.
- Use the official released Go MCP SDK without a fork. Implement the Skills methods using its generic custom-method and resource APIs until first-class Skills APIs are released.
- Test against the user's local pi agent build, which is being updated to support Skills over MCP.
- Use Streamable HTTP for the remote MCP endpoint.

As of October 7, 2026, the Go SDK's [Skills protocol PR #1238](https://github.com/modelcontextprotocol/go-sdk/pull/1238) was open and the companion [filesystem provider PR #1240](https://github.com/modelcontextprotocol/go-sdk/pull/1240) was a draft. These are useful design and implementation references, not dependencies for this prototype. Reassess their status before implementation; keep the application catalog boundary independent of either PR.

### Skill source and updates

- The initial source is a configured directory. It may be a standalone skills directory or a directory within an existing Git checkout.
- The server does not clone or pull remote Git repositories in the prototype. GitOps or deployment automation is responsible for placing the desired checkout at the configured path.
- Load and validate the source into a consistent catalog snapshot. A refresh or restart publishes the new snapshot atomically, so a request never observes a mixture of revisions.
- Include the source revision in telemetry when it can be determined (for example, the current Git commit for a checkout).

### Source/catalog boundary

The MCP handlers must depend on a skills-oriented abstraction, not filesystem paths or filesystem calls. The directory implementation is one backend; a later Git or database implementation should be able to supply the same catalog behavior.

The initial catalog snapshot provides:

- A source revision identifier.
- Skill listing and lookup by skill URI.
- Resource reads by URI.
- Direct-child directory listings.
- Parsed frontmatter, complete manifests, and the exact bytes served for that snapshot.

Keep source loading separate from access-policy evaluation and telemetry. Do not make MCP request handlers responsible for scanning directories, parsing Git state, or understanding database storage.

### Authentication and authorization

- Use Keycloak as the initial OAuth/OIDC authorization server and implement the MCP HTTP resource-server requirements.
- Validate bearer tokens, including signature, issuer, audience, and expiration. Do not trust caller-supplied identity headers.
- Use the token's `iss` and `sub` as the stable user identity. Record client identity when present. Service-account tokens identify the service account, not an end user.
- Map a configurable Keycloak group/role claim to application entitlements.
- Store skill access requirements in a **separate policy file**, not in skill frontmatter. An access requirement is attached to a skill, and applies to its complete manifest and all of its files. If multiple groups are listed, prototype semantics are “any listed group grants access.”
- OAuth authentication is required for all requests. Skills without an explicit group restriction are available to any authenticated user; restricted skills require one of their configured groups.
- Enforce authorization on `skills/list`, `skills/get`, `resources/read`, and `resources/directory/read`. Filter inaccessible skills from listings; a client must not gain access by addressing a URI or file directly.
- Authorization is checked on every request. Auth-filtered list responses use private caching semantics; a cache hint is not an authorization decision.

The exact YAML spelling for the policy file and Keycloak claim path can be chosen during implementation, but the file remains independently managed from skill content. No admin UI is part of the prototype.

### Telemetry

Represent usage as OpenTelemetry spans and export them through the configured OTel exporter. Add the `user.id` attribute using the validated token identity (`iss` + `sub`, or a stable pseudonymous ID derived from them). Include client identity when available.

For skill-related spans, record the operation (list, get, resource read, directory read), skill/resource URI, allow/deny outcome, source revision or resource digest, and request/correlation context. Do not record bearer tokens or skill/resource contents. A read span establishes that content was requested; it does not prove that a host loaded the skill into model context or that a model used it.

## Runtime shape

The server composes four backend-independent concerns:

1. **Catalog source:** loads a directory into a validated snapshot.
2. **MCP Skills adapter:** advertises `resources` and `io.modelcontextprotocol/skills`; implements `skills/list`, `skills/get`, resource reads, and directory reads; emits protocol-version-appropriate result and cache fields.
3. **Authentication and policy:** builds a principal from the validated OAuth token and enforces the skill-level policy on every operation.
4. **Telemetry sink:** persists request and authorization events without recording content or tokens.

For each request, the intended flow is: authenticate the bearer token, resolve the caller's principal, authorize the requested skill, access the active catalog snapshot, return the conforming MCP response, and record the outcome.

## Implementation order

1. Establish the Streamable HTTP MCP server.
2. Build and validate the backend-neutral catalog snapshot.
3. Implement the Skills extension and resource/directory handlers against the catalog interface, including capability declaration, pagination, caching fields, and protocol errors. Verify them over MCP before adding authorization.
4. Establish the authenticated principal and enforce the separate skill-level access policy on every handler. Require authentication at the HTTP boundary and filter or deny access at the catalog-facing boundary, including the standard `resources/list` endpoint so it cannot reveal restricted skill files.
5. Add user-attributed telemetry and subsequent integration checks.

Step 3 exposes an unauthenticated prototype endpoint for protocol testing only; the authentication requirement above applies when step 4 is complete.

## Hosted-service path

The catalog boundary should permit a future database-backed implementation without changing MCP handlers. A hosted service could add tenant-scoped catalogs, a policy store and admin UI, and authoring/evaluation/review workflows.

Authoring and read access remain distinct. Users can submit proposed skill changes without being granted access to the source repository or to skills they are not allowed to read. Evaluations and review apply to proposed revisions; approval/promotion remains a separate capability from submitting changes or changing access policy.

## Out of scope for the prototype

- Cloning or polling remote Git repositories, webhook processing, and deployment orchestration.
- An admin UI or database-backed catalog/policy store.
- Per-file access controls, conditional skill templating, or task-context-dependent content.
- Skill submission, evaluation pipelines, review, and promotion.
- Claims that a fetched skill was activated or used by a model.

## Validation approach

- Run the Skills extension conformance scenarios against the server and verify interoperability with the local pi build.
- Verify frontmatter, URI/name agreement, complete manifests, byte sizes, SHA-256 digests, pagination, and protocol-version-specific response fields.
- Verify authorization is enforced on listing, metadata lookup, file reads, and directory reads, including attempts to access a restricted skill by URI directly.
- Verify a catalog refresh switches revisions atomically and that telemetry attributes events to the validated token identity without storing secrets or content.

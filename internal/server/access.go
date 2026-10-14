package server

import (
	"context"
	"errors"
	"net/url"
	"path"
	"sort"

	"github.com/Cali0707/mcpskills/internal/catalog"
	"github.com/Cali0707/mcpskills/internal/policy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type accessControl struct {
	source    catalog.Catalog
	policy    *policy.Policy
	resources []*mcp.Resource
	telemetry *telemetry
	digests   map[string]string
}

func newAccessControl(source catalog.Catalog, rules *policy.Policy) (*accessControl, error) {
	entries, err := source.ListSkills()
	if err != nil {
		return nil, err
	}
	a := &accessControl{source: source, policy: rules, digests: make(map[string]string)}
	seen := make(map[string]bool)
	for _, entry := range entries {
		for _, file := range entry.Resources {
			a.digests[file.URI] = file.Digest
			if seen[file.URI] {
				continue
			}
			seen[file.URI] = true
			u, err := url.Parse(file.URI)
			if err != nil {
				return nil, err
			}
			resource := &mcp.Resource{URI: file.URI, Name: path.Base(u.Path), MIMEType: resourceMIMEType(file.URI), Size: file.Size}
			if file.URI == entry.URI {
				if name, ok := entry.Frontmatter["name"].(string); ok {
					resource.Name = name
				}
				if description, ok := entry.Frontmatter["description"].(string); ok {
					resource.Description = description
				}
			}
			a.resources = append(a.resources, resource)
		}
	}
	sort.Slice(a.resources, func(i, j int) bool { return a.resources[i].URI < a.resources[j].URI })
	return a, nil
}

func (a *accessControl) allows(ctx context.Context, skillURI string) bool {
	if a == nil {
		return true // Protocol-only test server; Run always installs access control.
	}
	p, ok := principalFrom(ctx)
	return ok && a.policy.Allows(skillURI, p.groups)
}

func (a *accessControl) resourceAllowed(ctx context.Context, uri string) bool {
	if a == nil {
		return true
	}
	owner, err := a.source.ResourceSkill(uri)
	return err == nil && a.allows(ctx, owner)
}

func (a *accessControl) directoryAllowed(ctx context.Context, uri string) bool {
	if a == nil {
		return true
	}
	owner, err := a.source.DirectorySkill(uri)
	return err == nil && a.allows(ctx, owner)
}

func (a *accessControl) childAllowed(ctx context.Context, child catalog.DirectoryEntry) bool {
	if child.MIMEType == "inode/directory" {
		return a.directoryAllowed(ctx, child.URI)
	}
	return a.resourceAllowed(ctx, child.URI)
}

// resources/list is implemented by the SDK from a global registry. Intercept it
// before SDK pagination so a restricted resource cannot appear on any page.
func (a *accessControl) middleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		uri := ""
		switch p := req.GetParams().(type) {
		case *uriParams:
			uri = p.URI
		case *directoryParams:
			uri = p.URI
		case *mcp.ReadResourceParams:
			uri = p.URI
		}
		if a.telemetry != nil && (method == "skills/list" || method == "skills/get" || method == "resources/list" || method == "resources/read" || method == "resources/directory/read") {
			var span trace.Span
			ctx, span = a.telemetry.tracer.Start(ctx, method)
			defer span.End()
			attrs := []attribute.KeyValue{attribute.String("mcp.operation", method), attribute.String("skill.source.revision", a.source.Revision())}
			if uri != "" {
				attrs = append(attrs, attribute.String("mcp.resource.uri", uri))
			}
			if digest := a.digests[uri]; digest != "" {
				attrs = append(attrs, attribute.String("mcp.resource.digest", digest))
			}
			if p, ok := principalFrom(ctx); ok {
				attrs = append(attrs, attribute.String("user.id", p.userID))
				if p.clientID != "" {
					attrs = append(attrs, attribute.String("client.id", p.clientID))
				}
			}
			defer func() { span.SetAttributes(attrs...) }()
		}
		result, err := a.handleMethod(ctx, method, req, next)
		if a.telemetry != nil {
			if method == "resources/read" && err == nil && a.telemetry.readCounter != nil {
				if skillURI, ownerErr := a.source.ResourceSkill(uri); ownerErr == nil {
					a.telemetry.readCounter.Add(ctx, 1, metric.WithAttributes(
						attribute.String("mcp.skill.uri", skillURI),
						attribute.String("mcp.resource.uri", uri),
						attribute.String("skill.source.revision", a.source.Revision()),
					))
				}
			}
			// The outcome is based on the policy, independently of whether the
			// response uses an indistinguishable not-found protocol error.
			allowed := true
			switch method {
			case "skills/get":
				allowed = a.allows(ctx, uri)
			case "resources/read":
				allowed = a.resourceAllowed(ctx, uri)
			case "resources/directory/read":
				allowed = a.directoryAllowed(ctx, uri)
			}
			outcome := "allow"
			if !allowed {
				outcome = "deny"
			} else if err != nil {
				outcome = "error"
			}
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("skill.access.outcome", outcome))
		}
		return result, err
	}
}

func (a *accessControl) handleMethod(ctx context.Context, method string, req mcp.Request, next mcp.MethodHandler) (mcp.Result, error) {
	if method != "resources/list" {
		return next(ctx, method, req)
	}
	p, ok := principalFrom(ctx)
	if !ok {
		return nil, errors.New("unauthenticated resource listing")
	}
	visible := make([]*mcp.Resource, 0, len(a.resources))
	for _, resource := range a.resources {
		owner, err := a.source.ResourceSkill(resource.URI)
		if err == nil && a.policy.Allows(owner, p.groups) {
			visible = append(visible, resource)
		}
	}
	cursor := ""
	if params, ok := req.GetParams().(*mcp.ListResourcesParams); ok && params != nil {
		cursor = params.Cursor
	}
	start, end, nextCursor, err := page(cursor, len(visible))
	if err != nil {
		return nil, mcp.ResourceNotFoundError("invalid resources cursor")
	}
	return &mcp.ListResourcesResult{Resources: visible[start:end], NextCursor: nextCursor, Cacheable: mcp.Cacheable{TTLMs: cacheTTL, CacheScope: "private"}}, nil
}

func (a *accessControl) cacheable() mcp.Cacheable {
	if a == nil {
		return mcp.Cacheable{TTLMs: cacheTTL, CacheScope: "public"}
	}
	return mcp.Cacheable{TTLMs: cacheTTL, CacheScope: "private"}
}

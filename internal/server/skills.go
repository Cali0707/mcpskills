package server

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Cali0707/mcpskills/internal/catalog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	skillsExtension = "io.modelcontextprotocol/skills"
	pageSize        = 50
	cacheTTL        = 300000
)

type listParams struct {
	mcp.ParamsBase
	Cursor string `json:"cursor,omitempty"`
}

type uriParams struct {
	mcp.ParamsBase
	URI string `json:"uri"`
}

type listSkillsResult struct {
	mcp.ResultBase
	ResultType string               `json:"resultType"`
	Skills     []catalog.SkillEntry `json:"skills"`
	NextCursor string               `json:"nextCursor,omitempty"`
	mcp.Cacheable
}

type getSkillResult struct {
	mcp.ResultBase
	ResultType string             `json:"resultType"`
	Skill      catalog.SkillEntry `json:"skill"`
	mcp.Cacheable
}

type directoryResult struct {
	mcp.ResultBase
	ResultType string                   `json:"resultType"`
	Resources  []catalog.DirectoryEntry `json:"resources"`
	NextCursor string                   `json:"nextCursor,omitempty"`
}

func registerSkills(s *mcp.Server, source catalog.Catalog, access *accessControl) error {
	if source == nil {
		return errors.New("skills catalog is required")
	}
	entries, err := source.ListSkills()
	if err != nil {
		return fmt.Errorf("list catalog skills: %w", err)
	}
	if err := mcp.AddReceivingCustomMethod(s, "skills/list", func(ctx context.Context, _ *mcp.ServerSession, p *listParams) (*listSkillsResult, error) {
		visible := make([]catalog.SkillEntry, 0, len(entries))
		for _, entry := range entries {
			if access.allows(ctx, entry.URI) {
				visible = append(visible, entry)
			}
		}
		start, end, next, err := page(p.Cursor, len(visible))
		if err != nil {
			return nil, mcp.ResourceNotFoundError("invalid skills cursor")
		}
		return &listSkillsResult{
			ResultType: "complete", Skills: append([]catalog.SkillEntry{}, visible[start:end]...), NextCursor: next,
			Cacheable: access.cacheable(),
		}, nil
	}); err != nil {
		return err
	}
	if err := mcp.AddReceivingCustomMethod(s, "skills/get", func(ctx context.Context, _ *mcp.ServerSession, p *uriParams) (*getSkillResult, error) {
		if !access.allows(ctx, p.URI) {
			return nil, mcp.ResourceNotFoundError(p.URI)
		}
		entry, err := source.GetSkill(p.URI)
		if errors.Is(err, catalog.ErrSkillNotFound) {
			return nil, mcp.ResourceNotFoundError(p.URI)
		}
		if err != nil {
			return nil, fmt.Errorf("get skill: %w", err)
		}
		return &getSkillResult{ResultType: "complete", Skill: entry, Cacheable: access.cacheable()}, nil
	}); err != nil {
		return err
	}
	if err := mcp.AddReceivingCustomMethod(s, "resources/directory/read", func(ctx context.Context, _ *mcp.ServerSession, p *directoryParams) (*directoryResult, error) {
		if !access.directoryAllowed(ctx, p.URI) {
			return nil, mcp.ResourceNotFoundError(p.URI)
		}
		children, err := source.ReadDirectory(p.URI)
		if errors.Is(err, catalog.ErrDirectoryNotFound) {
			return nil, mcp.ResourceNotFoundError(p.URI)
		}
		if err != nil {
			return nil, fmt.Errorf("read directory: %w", err)
		}
		visible := make([]catalog.DirectoryEntry, 0, len(children))
		for _, child := range children {
			if access.childAllowed(ctx, child) {
				visible = append(visible, child)
			}
		}
		start, end, next, err := page(p.Cursor, len(visible))
		if err != nil {
			return nil, mcp.ResourceNotFoundError("invalid directory cursor")
		}
		return &directoryResult{ResultType: "complete", Resources: append([]catalog.DirectoryEntry{}, visible[start:end]...), NextCursor: next}, nil
	}); err != nil {
		return err
	}

	seen := make(map[string]bool)
	for _, entry := range entries {
		for _, file := range entry.Resources {
			if seen[file.URI] {
				continue
			}
			seen[file.URI] = true
			uri := file.URI
			name, err := url.Parse(uri)
			if err != nil {
				return fmt.Errorf("invalid resource URI %q: %w", uri, err)
			}
			resource := &mcp.Resource{URI: uri, Name: path.Base(name.Path), MIMEType: resourceMIMEType(uri), Size: file.Size}
			if uri == entry.URI {
				if label, ok := entry.Frontmatter["name"].(string); ok {
					resource.Name = label
				}
				if description, ok := entry.Frontmatter["description"].(string); ok {
					resource.Description = description
				}
			}
			s.AddResource(resource, func(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				if !access.resourceAllowed(ctx, request.Params.URI) {
					return nil, mcp.ResourceNotFoundError(request.Params.URI)
				}
				content, err := source.ReadResource(request.Params.URI)
				if errors.Is(err, catalog.ErrResourceNotFound) {
					return nil, mcp.ResourceNotFoundError(request.Params.URI)
				}
				if err != nil {
					return nil, fmt.Errorf("read resource: %w", err)
				}
				body := &mcp.ResourceContents{URI: request.Params.URI, MIMEType: resource.MIMEType}
				if len(content) > 0 && utf8.Valid(content) && !strings.ContainsRune(string(content), 0) {
					body.Text = string(content)
				} else {
					body.Blob = content
				}
				return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{body}, Cacheable: access.cacheable()}, nil
			})
		}
	}
	return nil
}

type directoryParams struct {
	mcp.ParamsBase
	URI    string `json:"uri"`
	Cursor string `json:"cursor,omitempty"`
}

func page(cursor string, length int) (start, end int, next string, err error) {
	if cursor != "" {
		start, err = strconv.Atoi(cursor)
		if err != nil || start < 1 || start >= length || strconv.Itoa(start) != cursor || start%pageSize != 0 {
			return 0, 0, "", errors.New("invalid cursor")
		}
	}
	end = min(start+pageSize, length)
	if end < length {
		next = strconv.Itoa(end)
	}
	return start, end, next, nil
}

func resourceMIMEType(uri string) string {
	ext := path.Ext(uri)
	if strings.EqualFold(ext, ".md") {
		return "text/markdown"
	}
	if value := mime.TypeByExtension(ext); value != "" {
		return value
	}
	return "application/octet-stream"
}

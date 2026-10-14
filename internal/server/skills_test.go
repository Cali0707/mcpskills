package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Cali0707/mcpskills/internal/catalog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type skillsClient struct {
	t       *testing.T
	ctx     context.Context
	session *mcp.ClientSession
}

func fixtureCatalog(t *testing.T) catalog.Catalog {
	t.Helper()
	source, err := catalog.NewDirectorySource(filepath.Join("..", "catalog", "testdata", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func connectSkillsClient(t *testing.T, source catalog.Catalog) *skillsClient {
	t.Helper()
	srv, err := newMCPServer(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(newHTTPHandler(srv, testEndpointPath, nil))
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "skills-test", Version: "test"}, nil)
	if err := mcp.AddSendingCustomMethod[*listParams, *listSkillsResult](client, "skills/list"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddSendingCustomMethod[*uriParams, *getSkillResult](client, "skills/get"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddSendingCustomMethod[*directoryParams, *directoryResult](client, "resources/directory/read"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL + testEndpointPath, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return &skillsClient{t: t, ctx: ctx, session: session}
}

func (c *skillsClient) list(cursor string) *listSkillsResult {
	c.t.Helper()
	result, err := mcp.CallCustomMethod[*listParams, *listSkillsResult](c.ctx, c.session, "skills/list", &listParams{Cursor: cursor})
	if err != nil {
		c.t.Fatal(err)
	}
	return result
}

func (c *skillsClient) get(uri string) *getSkillResult {
	c.t.Helper()
	result, err := mcp.CallCustomMethod[*uriParams, *getSkillResult](c.ctx, c.session, "skills/get", &uriParams{URI: uri})
	if err != nil {
		c.t.Fatal(err)
	}
	return result
}

func (c *skillsClient) directory(uri, cursor string) *directoryResult {
	c.t.Helper()
	result, err := mcp.CallCustomMethod[*directoryParams, *directoryResult](c.ctx, c.session, "resources/directory/read", &directoryParams{URI: uri, Cursor: cursor})
	if err != nil {
		c.t.Fatal(err)
	}
	return result
}

func (c *skillsClient) read(uri string) *mcp.ReadResourceResult {
	c.t.Helper()
	result, err := c.session.ReadResource(c.ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		c.t.Fatal(err)
	}
	return result
}

func TestSkillsListAndGetExposeCompleteEntries(t *testing.T) {
	client := connectSkillsClient(t, fixtureCatalog(t))
	listed := client.list("")
	if listed.ResultType != "complete" || len(listed.Skills) != 2 || listed.NextCursor != "" {
		t.Fatalf("list should contain two complete skill entries: %+v", listed)
	}
	if listed.TTLMs != cacheTTL || listed.CacheScope != "public" {
		t.Errorf("list cache hints = %d, %q", listed.TTLMs, listed.CacheScope)
	}
	for _, skill := range listed.Skills {
		t.Run(skill.URI, func(t *testing.T) {
			got := client.get(skill.URI)
			if got.ResultType != "complete" || got.Skill.URI != skill.URI || got.TTLMs != cacheTTL || got.CacheScope != "public" {
				t.Fatalf("get should return complete metadata and cache hints: %+v", got)
			}
			if !reflect.DeepEqual(got.Skill, skill) {
				t.Errorf("get entry does not match listed entry: %+v", got.Skill)
			}
		})
	}
}

func TestResourceReadsMatchPublishedManifest(t *testing.T) {
	client := connectSkillsClient(t, fixtureCatalog(t))
	for _, skill := range client.list("").Skills {
		for _, file := range skill.Resources {
			t.Run(file.URI, func(t *testing.T) {
				read := client.read(file.URI)
				if len(read.Contents) != 1 || read.Contents[0].URI != file.URI {
					t.Fatalf("read should return the requested resource: %+v", read)
				}
				content := []byte(read.Contents[0].Text)
				if read.Contents[0].Blob != nil {
					content = read.Contents[0].Blob
				}
				digest := sha256.Sum256(content)
				if int64(len(content)) != file.Size || "sha256:"+hex.EncodeToString(digest[:]) != file.Digest {
					t.Errorf("served bytes do not match published size and digest")
				}
			})
		}
	}
}

func TestDirectoryReadListsDirectChildren(t *testing.T) {
	client := connectSkillsClient(t, fixtureCatalog(t))
	root := client.directory("skill://toolbox", "")
	if root.ResultType != "complete" {
		t.Fatalf("directory result type = %q", root.ResultType)
	}
	want := map[string]string{
		"skill://toolbox/SKILL.md":    "text/markdown",
		"skill://toolbox/references":  "inode/directory",
		"skill://toolbox/nested-tool": "inode/directory",
	}
	for _, child := range root.Resources {
		if mimeType, ok := want[child.URI]; !ok || mimeType != child.MIMEType {
			t.Errorf("unexpected root child: %+v", child)
		}
		delete(want, child.URI)
	}
	if len(want) != 0 {
		t.Errorf("missing root children: %v", want)
	}
	references := client.directory("skill://toolbox/references", "")
	if len(references.Resources) != 1 || references.Resources[0].URI != "skill://toolbox/references/guide.md" {
		t.Errorf("references should list only its direct child: %+v", references.Resources)
	}
}

func TestUnknownSkillResourceAndDirectoryAreRejected(t *testing.T) {
	client := connectSkillsClient(t, fixtureCatalog(t))
	for _, uri := range []string{"skill://missing/SKILL.md", "skill://toolbox/references/guide.md"} {
		t.Run("get "+uri, func(t *testing.T) {
			if _, err := mcp.CallCustomMethod[*uriParams, *getSkillResult](client.ctx, client.session, "skills/get", &uriParams{URI: uri}); err == nil {
				t.Error("skills/get accepted a non-skill URI")
			}
		})
	}
	if _, err := mcp.CallCustomMethod[*directoryParams, *directoryResult](client.ctx, client.session, "resources/directory/read", &directoryParams{URI: "skill://toolbox/SKILL.md"}); err == nil {
		t.Error("directory/read accepted a file URI")
	}
	if _, err := client.session.ReadResource(client.ctx, &mcp.ReadResourceParams{URI: "skill://toolbox/missing.txt"}); err == nil {
		t.Error("resources/read accepted a missing file URI")
	}
}

func catalogWithSkills(t *testing.T, count int) catalog.Catalog {
	t.Helper()
	root := t.TempDir()
	for i := range count {
		name := fmt.Sprintf("skill-%03d", i)
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Test\n---\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source, err := catalog.NewDirectorySource(root)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func catalogWithFiles(t *testing.T, name string, files map[string][]byte) catalog.Catalog {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Test\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for file, content := range files {
		if err := os.WriteFile(filepath.Join(dir, file), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	source, err := catalog.NewDirectorySource(root)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestSkillsListPagination(t *testing.T) {
	client := connectSkillsClient(t, catalogWithSkills(t, pageSize+2))
	first := client.list("")
	if len(first.Skills) != pageSize || first.NextCursor != "50" {
		t.Fatalf("first page should contain 50 skills and a cursor: %+v", first)
	}
	second := client.list(first.NextCursor)
	if len(second.Skills) != 2 || second.NextCursor != "" || second.Skills[0].URI == first.Skills[0].URI {
		t.Fatalf("second page should contain the remaining distinct skills: %+v", second)
	}
	if _, err := mcp.CallCustomMethod[*listParams, *listSkillsResult](client.ctx, client.session, "skills/list", &listParams{Cursor: "1"}); err == nil {
		t.Error("list accepted a cursor that was not issued by a page boundary")
	}
	if got := client.get(second.Skills[0].URI); got.Skill.URI != second.Skills[0].URI {
		t.Errorf("skills/get could not retrieve a skill on a later page: %+v", got.Skill)
	}
}

func TestDirectoryReadPaginationAndEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "many-files")
	if err := os.Mkdir(skillDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: many-files\ndescription: Test\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(skillDir, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	for i := range pageSize {
		name := fmt.Sprintf("file-%03d.txt", i)
		if err := os.WriteFile(filepath.Join(skillDir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source, err := catalog.NewDirectorySource(root)
	if err != nil {
		t.Fatal(err)
	}
	client := connectSkillsClient(t, source)
	first := client.directory("skill://many-files", "")
	if len(first.Resources) != pageSize || first.NextCursor != "50" {
		t.Fatalf("first directory page should include 50 children and a cursor: %+v", first)
	}
	second := client.directory("skill://many-files", first.NextCursor)
	if len(second.Resources) != 2 || second.NextCursor != "" {
		t.Fatalf("second directory page should contain the remaining children: %+v", second)
	}
	empty := client.directory("skill://many-files/empty", "")
	if empty.Resources == nil || len(empty.Resources) != 0 || empty.NextCursor != "" {
		t.Fatalf("empty directory should have an empty resource array: %+v", empty)
	}
}

func TestBinaryResourceUsesBlob(t *testing.T) {
	binary := []byte{0, 0xff, 0x01}
	client := connectSkillsClient(t, catalogWithFiles(t, "images", map[string][]byte{"image.bin": binary}))
	read := client.read("skill://images/image.bin")
	if len(read.Contents) != 1 || !bytes.Equal(read.Contents[0].Blob, binary) || read.Contents[0].Text != "" {
		t.Fatalf("binary resource should be returned as a blob: %+v", read)
	}
}

func TestEmptyResourceReturnsAContentField(t *testing.T) {
	client := connectSkillsClient(t, catalogWithFiles(t, "empty-file", map[string][]byte{"zero.bin": {}}))
	read := client.read("skill://empty-file/zero.bin")
	if len(read.Contents) != 1 || read.Contents[0].Blob == nil || len(read.Contents[0].Blob) != 0 {
		t.Fatalf("empty file should be returned as an empty blob: %+v", read)
	}
}

type failingCatalog struct{ catalog.Catalog }

func (failingCatalog) ListSkills() ([]catalog.SkillEntry, error) {
	return nil, errors.New("catalog unavailable")
}

func TestServerRejectsCatalogFailure(t *testing.T) {
	if _, err := newMCPServer(failingCatalog{}, nil); err == nil {
		t.Fatal("server accepted a failed catalog")
	}
}

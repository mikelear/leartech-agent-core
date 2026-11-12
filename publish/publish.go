// Package publish is the artifact PUBLISH lifecycle, shared by the shell and the web
// console: create (or revise) an artifact via the design/artifact MCP, upload each
// file's bytes DIRECT to GCS over the presigned PUT, then finalize (seal) the
// revision.
//
// WHY SHARED. The shell's `design publish` and the web console's workspace__publish
// do the identical three steps and must obey the identical invariant — the bytes go
// direct to GCS, never through the MCP (artifact-api is a control plane). Lifting the
// lifecycle here means one implementation of that invariant with a second consumer
// (the rule we've used for remotemcp + sessionkey), and the server-side restrictions
// (size/type/sha) stay in artifact-api where they already live.
package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// File is one file to publish: its path within the revision and its bytes. SHA256 and
// the byte count are computed here so a caller passes only path+body.
type File struct {
	Path string
	Body []byte
}

// Result reports what was sealed.
type Result struct {
	ArtifactID string
	Revision   int
	Files      int
}

// fileArg is the manifest entry sent to create/finalize (path + sha + size).
type fileArg struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
	Bytes  int    `json:"bytes,omitempty"`
}

// createOut is the subset of the create_artifact / create_revision response the
// lifecycle needs: the id, revision, and the presigned upload URLs (one per file, or
// the single-file uploadURL).
type createOut struct {
	ID         string `json:"id"`
	Revision   int    `json:"revision"`
	UploadURL  string `json:"uploadURL"`
	UploadURLs []struct {
		Path string `json:"path"`
		URL  string `json:"url"`
	} `json:"uploadURLs"`
}

// Options are the publish inputs beyond the files + session.
type Options struct {
	Label      string // required: the artifact's discovery label
	Kind       string // e.g. design-doc, video-bundle
	Target     string // optional consumer hint
	Producer   string // recorded on the sealed revision
	ExistingID string // when set, a new REVISION of this artifact instead of a new one
}

// Publish runs the whole lifecycle against an already-connected design/artifact MCP
// session and an HTTP client for the presigned PUTs (nil → http.DefaultClient).
// Returns the sealed artifact id + revision.
//
// proven-by: publish_test.go
func Publish(ctx context.Context, sess *mcp.ClientSession, httpClient *http.Client, files []File, opt Options) (Result, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if len(files) == 0 {
		return Result{}, fmt.Errorf("publish: no files")
	}
	if opt.Label == "" {
		return Result{}, fmt.Errorf("publish: a label is required")
	}

	args := make([]fileArg, len(files))
	for i, f := range files {
		args[i] = fileArg{Path: f.Path, SHA256: sha256hex(f.Body), Bytes: len(f.Body)}
	}

	// 1. create (or revise)
	toolName := "create_artifact"
	in := map[string]any{"label": opt.Label, "kind": opt.Kind, "files": args}
	if opt.Target != "" {
		in["target"] = opt.Target
	}
	if opt.ExistingID != "" {
		toolName = "create_revision"
		in["artifact_id"] = opt.ExistingID
	}
	created, err := callTool[createOut](ctx, sess, toolName, in)
	if err != nil {
		return Result{}, err
	}

	// 2. PUT each file's bytes DIRECT to GCS (never through the MCP).
	urlByPath := map[string]string{}
	for _, u := range created.UploadURLs {
		urlByPath[u.Path] = u.URL
	}
	if len(urlByPath) == 0 && created.UploadURL != "" && len(files) == 1 {
		urlByPath[files[0].Path] = created.UploadURL
	}
	for _, f := range files {
		u, ok := urlByPath[f.Path]
		if !ok {
			return Result{}, fmt.Errorf("the artifact service returned no upload URL for %s — nothing was uploaded", f.Path)
		}
		if err := put(ctx, httpClient, u, f.Body); err != nil {
			return Result{}, fmt.Errorf("%s: %w", f.Path, err)
		}
	}

	// 3. finalize (seal)
	if _, err := callTool[json.RawMessage](ctx, sess, "finalize_artifact", map[string]any{
		"artifact_id": created.ID, "revision": created.Revision,
		"producer": opt.Producer, "files": args,
	}); err != nil {
		return Result{}, err
	}
	return Result{ArtifactID: created.ID, Revision: created.Revision, Files: len(files)}, nil
}

// put uploads one file's bytes to its presigned URL. THE BYTES GO DIRECT TO GCS, not
// through the MCP — artifact-api's invariant (a control-plane service does not proxy
// bulk data). Context-bound so a hung upload cancels with the caller.
func put(ctx context.Context, c *http.Client, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("uploading to the presigned URL: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
		return fmt.Errorf("the presigned upload returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// callTool invokes one MCP tool and decodes its single text content block into T.
func callTool[T any](ctx context.Context, sess *mcp.ClientSession, name string, args map[string]any) (T, error) {
	var zero T
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return zero, fmt.Errorf("%s: %w", name, err)
	}
	if res.IsError {
		return zero, fmt.Errorf("%s: %s", name, contentText(res.Content))
	}
	body := contentText(res.Content)
	if body == "" {
		return zero, nil
	}
	var out T
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return zero, fmt.Errorf("%s: decoding the response: %w", name, err)
	}
	return out, nil
}

func contentText(blocks []mcp.Content) string {
	for _, c := range blocks {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

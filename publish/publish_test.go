package publish_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikelear/leartech-agent-core/mockmcp"
	"github.com/mikelear/leartech-agent-core/publish"
)

func TestPublish_CreatePutFinalize(t *testing.T) {
	puts := map[string][]byte{}
	gcs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		puts[r.URL.Path] = b
		w.WriteHeader(200)
	}))
	defer gcs.Close()

	createResult := `{"id":"art-1","revision":1,"uploadURLs":[` +
		`{"path":"DESIGN.md","url":"` + gcs.URL + `/DESIGN.md"},` +
		`{"path":"a.html","url":"` + gcs.URL + `/a.html"}]}`
	cs := mockmcp.Session(t, []mockmcp.Tool{
		{Name: "create_artifact", Result: createResult},
		{Name: "finalize_artifact", Result: `{"state":"sealed"}`},
	}, mockmcp.Options{})

	res, err := publish.Publish(context.Background(), cs, gcs.Client(),
		[]publish.File{
			{Path: "DESIGN.md", Body: []byte("# design")},
			{Path: "a.html", Body: []byte("<h1>A</h1>")},
		},
		publish.Options{Label: "ship-proven/x", Kind: "design-doc", Producer: "test"},
	)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.ArtifactID != "art-1" || res.Revision != 1 || res.Files != 2 {
		t.Fatalf("result = %+v", res)
	}
	if string(puts["/DESIGN.md"]) != "# design" || string(puts["/a.html"]) != "<h1>A</h1>" {
		t.Fatalf("files not PUT to GCS: %v", puts)
	}
}

func TestPublish_RequiresLabelAndFiles(t *testing.T) {
	cs := mockmcp.Session(t, []mockmcp.Tool{}, mockmcp.Options{})
	if _, err := publish.Publish(context.Background(), cs, nil, nil, publish.Options{Label: "x"}); err == nil {
		t.Fatal("no files must error")
	}
	if _, err := publish.Publish(context.Background(), cs, nil, []publish.File{{Path: "a", Body: []byte("b")}}, publish.Options{}); err == nil {
		t.Fatal("no label must error")
	}
}

func TestPublish_AMissingUploadURLIsAnError(t *testing.T) {
	cs := mockmcp.Session(t, []mockmcp.Tool{
		{Name: "create_artifact", Result: `{"id":"a","revision":1,"uploadURLs":[]}`},
	}, mockmcp.Options{})
	_, err := publish.Publish(context.Background(), cs, http.DefaultClient,
		[]publish.File{{Path: "a.html", Body: []byte("x")}}, publish.Options{Label: "l"})
	if err == nil || !strings.Contains(err.Error(), "no upload URL") {
		t.Fatalf("expected a missing-upload-URL error, got %v", err)
	}
}

func TestPublish_RevisionUsesCreateRevision(t *testing.T) {
	gcs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer gcs.Close()
	cs := mockmcp.Session(t, []mockmcp.Tool{
		{Name: "create_revision", Result: `{"id":"art-1","revision":2,"uploadURLs":[{"path":"a.html","url":"` + gcs.URL + `/a"}]}`},
		{Name: "finalize_artifact", Result: `{"state":"sealed"}`},
	}, mockmcp.Options{})
	res, err := publish.Publish(context.Background(), cs, gcs.Client(),
		[]publish.File{{Path: "a.html", Body: []byte("x")}},
		publish.Options{Label: "l", ExistingID: "art-1"})
	if err != nil || res.Revision != 2 {
		t.Fatalf("revision publish: res=%+v err=%v", res, err)
	}
}

package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/barats/xlistman/internal/config"
	xmail "github.com/barats/xlistman/internal/mail"
	"github.com/barats/xlistman/internal/model"
	"github.com/barats/xlistman/internal/store/sqlite"
)

const testShell = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>xListman</title><meta name="description" content="default desc"><link href="/_app/a.css" rel="stylesheet"></head><body><div id="app"></div></body></html>`

func seoServer(t *testing.T, siteName string) *httptest.Server {
	t.Helper()
	web := fstest.MapFS{
		"web/build/index.html": {Data: []byte(testShell)},
		"web/build/_app/a.js":  {Data: []byte("console.log('asset')")},
	}
	st, err := sqlite.OpenInMemory()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if _, err := st.CreateDomain(ctx, "example.com", "Example"); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	d, _ := st.GetDomain(ctx, "example.com")
	if _, err := st.CreateList(ctx, "dev", d.ID, "example.com", "Development list", model.ListTypeDiscussion); err != nil {
		t.Fatalf("create list: %v", err)
	}
	// Second list without a description exercises the fallback description.
	if _, err := st.CreateList(ctx, "team", d.ID, "example.com", "", model.ListTypeNewsletter); err != nil {
		t.Fatalf("create list: %v", err)
	}
	cfg := &config.Config{Web: config.WebConfig{BaseURL: "http://test.local", SiteName: siteName}}
	pipeline := &xmail.Pipeline{Store: st, WebBaseURL: cfg.Web.BaseURL}
	srv := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), pipeline, "test", web)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func getBody(t *testing.T, ts *httptest.Server, path string) string {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func mustContain(t *testing.T, body, needle string) {
	t.Helper()
	if !strings.Contains(body, needle) {
		t.Errorf("body missing %q\n%s", needle, body)
	}
}

func mustNotContain(t *testing.T, body, needle string) {
	t.Helper()
	if strings.Contains(body, needle) {
		t.Errorf("body unexpectedly contains %q\n%s", needle, body)
	}
}

// assertSingleTag fails the test if more than one opening of `tagOpen`
// (e.g. `<title>`, `<meta name="description"`) appears in body. Catches
// duplicate tags from the injector when a tag was replaced then re-appended.
func assertSingleTag(t *testing.T, body, tagOpen string) {
	t.Helper()
	if n := strings.Count(body, tagOpen); n != 1 {
		t.Errorf("%s count = %d, want 1\n%s", tagOpen, n, body)
	}
}

func TestSEOInjection_Routes(t *testing.T) {
	ts := seoServer(t, "xListman")

	t.Run("list index", func(t *testing.T) {
		body := getBody(t, ts, "/")
		mustContain(t, body, "<title>Mailing lists — xListman</title>")
		mustContain(t, body, `name="description" content="Browse the mailing lists hosted on this xListman instance and subscribe with one email address."`)
		mustContain(t, body, `name="xlistman-site-name" content="xListman"`)
		mustContain(t, body, `<link rel="canonical" href="http://test.local/">`)
		mustContain(t, body, `name="xlistman-version" content="test"`)
		assertSingleTag(t, body, `name="xlistman-version"`)
		mustContain(t, body, `property="og:title" content="Mailing lists — xListman"`)
		mustContain(t, body, `property="og:url" content="http://test.local/"`)
		mustContain(t, body, `property="og:site_name" content="xListman"`)
		mustContain(t, body, `property="og:image" content="http://test.local/og-image.png"`)
		mustContain(t, body, `property="og:image:width" content="1200"`)
		mustContain(t, body, `property="og:image:height" content="630"`)
		mustContain(t, body, `name="twitter:card" content="summary_large_image"`)
		mustContain(t, body, `name="twitter:image" content="http://test.local/og-image.png"`)
		mustNotContain(t, body, `name="robots" content="noindex"`)
		// The shell's default title is replaced, not duplicated.
		assertSingleTag(t, body, "<title>")
	})

	t.Run("list page", func(t *testing.T) {
		body := getBody(t, ts, "/l/dev@example.com")
		mustContain(t, body, "<title>dev@example.com — xListman</title>")
		mustContain(t, body, `name="description" content="Development list"`)
		mustContain(t, body, `<link rel="canonical" href="http://test.local/l/dev@example.com"`)
		mustContain(t, body, `property="og:url" content="http://test.local/l/dev@example.com"`)
		mustNotContain(t, body, `name="robots" content="noindex"`)
	})

	t.Run("list without description", func(t *testing.T) {
		body := getBody(t, ts, "/l/team@example.com")
		mustContain(t, body, "<title>team@example.com — xListman</title>")
		mustContain(t, body, `name="description" content="Subscribe to the team@example.com mailing list."`)
	})

	t.Run("missing list", func(t *testing.T) {
		body := getBody(t, ts, "/l/nope@example.com")
		mustContain(t, body, "<title>List not found — xListman</title>")
		mustContain(t, body, `name="robots" content="noindex"`)
		mustNotContain(t, body, `property="og:title"`)
		mustNotContain(t, body, `property="og:image"`)
	})

	t.Run("nested members-only page", func(t *testing.T) {
		body := getBody(t, ts, "/l/dev@example.com/archives")
		mustContain(t, body, `name="robots" content="noindex"`)
		mustNotContain(t, body, `<link rel="canonical"`)
		mustNotContain(t, body, `property="og:title"`)
	})

	t.Run("private route gets shell default", func(t *testing.T) {
		body := getBody(t, ts, "/me")
		mustContain(t, body, "<title>xListman</title>")
		mustContain(t, body, `name="xlistman-site-name" content="xListman"`)
		mustNotContain(t, body, `property="og:title"`)
		mustNotContain(t, body, `name="robots" content="noindex"`) // client adds noindex
	})

	t.Run("assets served unchanged", func(t *testing.T) {
		body := getBody(t, ts, "/_app/a.js")
		mustContain(t, body, "console.log('asset')")
		mustNotContain(t, body, "xlistman-site-name")
	})
}

func TestSEOInjection_ConfigurableSiteName(t *testing.T) {
	ts := seoServer(t, "My Community")
	body := getBody(t, ts, "/")
	mustContain(t, body, "<title>Mailing lists — My Community</title>")
	mustContain(t, body, `name="xlistman-site-name" content="My Community"`)
	mustContain(t, body, `property="og:site_name" content="My Community"`)
}

func TestSEOInjection_BrandAssets(t *testing.T) {
	ts := seoServer(t, "xListman")

	t.Run("favicon + theme-color on every route", func(t *testing.T) {
		for _, path := range []string{"/", "/l/dev@example.com", "/me", "/l/nope@example.com"} {
			body := getBody(t, ts, path)
			// PNG favicons cover all browsers; no SVG favicon is served.
			mustContain(t, body, `<link rel="icon" type="image/png" sizes="32x32" href="/favicon-32.png">`)
			mustContain(t, body, `<link rel="icon" type="image/png" sizes="16x16" href="/favicon-16.png">`)
			mustContain(t, body, `<link rel="apple-touch-icon" sizes="180x180" href="/apple-touch-icon.png">`)
			mustContain(t, body, `<meta name="theme-color" content="#053776">`)
			// No duplicate favicon / theme-color tags (the shell carries
			// static versions of these too; the injector must replace
			// them rather than append).
			assertSingleTag(t, body, `name="theme-color"`)
			assertSingleTag(t, body, `rel="apple-touch-icon"`)
			assertSingleTag(t, body, `sizes="32x32"`)
			assertSingleTag(t, body, `sizes="16x16"`)
		}
	})

	t.Run("og:image is the configured base URL plus /og-image.png", func(t *testing.T) {
		ts2 := seoServer(t, "xListman")
		body := getBody(t, ts2, "/")
		mustContain(t, body, `property="og:image" content="http://test.local/og-image.png"`)
		mustContain(t, body, `name="twitter:image" content="http://test.local/og-image.png"`)
	})

	t.Run("gated routes still get brand tags but no OG image", func(t *testing.T) {
		body := getBody(t, ts, "/me")
		mustContain(t, body, `<meta name="theme-color" content="#053776">`)
		mustContain(t, body, `<link rel="icon" type="image/png" sizes="32x32" href="/favicon-32.png">`)
		mustNotContain(t, body, `property="og:image"`)
	})
}

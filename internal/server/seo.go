package server

import (
	"html"
	"net/http"
	"strings"
)

// Brand assets served from the static directory. The Go server pre-injects
// absolute URLs into the head so the social-card image resolves correctly
// even when the SPA is served from a sub-path or behind a reverse proxy.
const (
	brandNavy      = "#053776"
	ogImagePath    = "/og-image.png"
	appleTouchPath = "/apple-touch-icon.png"
)

// seoTags are the per-route head elements injected into the SPA shell by
// spaHandler. Public routes (the list index and list pages) get Open Graph
// and canonical tags; every other route gets just a title and description,
// with gated or not-found pages flagged NoIndex.
type seoTags struct {
	Title       string
	Description string
	NoIndex     bool
	Canonical   string // absolute URL; empty for pages without public SEO
}

// seoTagsFor returns the head tags for a request path.
func (s *Server) seoTagsFor(r *http.Request) seoTags {
	site := s.Config.Web.SiteName
	if site == "" {
		site = "xMailman"
	}
	def := seoTags{
		Title:       site,
		Description: "A self-hosted, one-binary mailing list manager.",
	}
	switch {
	case r.URL.Path == "/":
		return seoTags{
			Title:       "Mailing lists — " + site,
			Description: "Browse the mailing lists hosted on this " + site + " instance and subscribe with one email address.",
			Canonical:   s.Config.Web.BaseURL + "/",
		}
	case strings.HasPrefix(r.URL.Path, "/l/"):
		return s.seoTagsForList(r, def)
	default:
		return def
	}
}

// seoTagsForList returns head tags for /l/{addr} and paths beneath it. The
// list page itself is public and gets list-derived tags; anything nested under
// a list (archives, held messages, the console) is gated and marked NoIndex.
func (s *Server) seoTagsForList(r *http.Request, def seoTags) seoTags {
	site := s.Config.Web.SiteName
	if site == "" {
		site = "xMailman"
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/l/"), "/")
	if strings.Contains(rest, "/") {
		// Nested pages (archives, held, console) are members-only.
		return seoTags{Title: def.Title, Description: def.Description, NoIndex: true}
	}
	at := strings.IndexByte(rest, '@')
	if at <= 0 || at == len(rest)-1 {
		return notFoundTags(site)
	}
	l, err := s.Store.GetList(r.Context(), rest[:at], rest[at+1:])
	if err != nil {
		return notFoundTags(site)
	}
	desc := l.Description
	if desc == "" {
		desc = "Subscribe to the " + rest + " mailing list."
	}
	return seoTags{
		Title:       rest + " — " + site,
		Description: desc,
		Canonical:   s.Config.Web.BaseURL + "/l/" + rest,
	}
}

func notFoundTags(site string) seoTags {
	return seoTags{
		Title:       "List not found — " + site,
		Description: "The mailing list you're looking for could not be found.",
		NoIndex:     true,
	}
}

// injectHead rewrites the served SPA shell head: it sets the title and
// description, the site-name marker the SPA reads at boot, noindex where the
// route is gated, favicon/theme-color brand tags for every route, and Open
// Graph / canonical tags for public routes. Tags already present in the
// shell are replaced; missing ones are inserted just before </head>.
func (s *Server) injectHead(h string, siteName string, seo seoTags) string {
	title := seo.Title
	if title == "" {
		title = siteName
	}
	h = setTag(h, "<title", "</title>", "<title>"+html.EscapeString(title)+"</title>")
	if seo.Description != "" {
		h = setTag(h, `name="description"`, "", `<meta name="description" content="`+html.EscapeString(seo.Description)+`">`)
	}
	h = setTag(h, `name="xmailman-site-name"`, "", `<meta name="xmailman-site-name" content="`+html.EscapeString(siteName)+`">`)
	h = setTag(h, `name="xmailman-version"`, "", `<meta name="xmailman-version" content="`+html.EscapeString(s.Version)+`">`)
	if seo.NoIndex {
		h = setTag(h, `name="robots"`, "", `<meta name="robots" content="noindex">`)
	} else {
		h = removeTag(h, `name="robots"`)
	}
	// Brand tags apply to every route (gated routes still get a tab icon and
	// a browser-chrome color). The shell's static tags are removed before
	// re-insertion so a stale favicon link or theme color never lingers.
	h = removeAllTags(h, `rel="icon"`)
	h = removeAllTags(h, `rel="apple-touch-icon"`)
	h = removeAllTags(h, `name="theme-color"`)
	// Insert brand tags right after </title> (or before </head> if no title).
	for _, tag := range []string{
		`<link rel="icon" type="image/png" sizes="32x32" href="/favicon-32.png">`,
		`<link rel="icon" type="image/png" sizes="16x16" href="/favicon-16.png">`,
		`<link rel="apple-touch-icon" sizes="180x180" href="` + appleTouchPath + `">`,
		`<meta name="theme-color" content="` + brandNavy + `">`,
	} {
		h = insertAfterTag(h, "</title>", tag)
	}
	if seo.Canonical != "" {
		h = setTag(h, `rel="canonical"`, "", `<link rel="canonical" href="`+html.EscapeString(seo.Canonical)+`">`)
		ogImage := s.baseURLOrEmpty() + ogImagePath
		og := []struct{ attr, content string }{
			{`property="og:title"`, title},
			{`property="og:description"`, seo.Description},
			{`property="og:type"`, "website"},
			{`property="og:url"`, seo.Canonical},
			{`property="og:site_name"`, siteName},
			{`property="og:image"`, ogImage},
			{`property="og:image:width"`, "1200"},
			{`property="og:image:height"`, "630"},
			{`property="og:image:alt"`, siteName + " logo"},
			{`name="twitter:card"`, "summary_large_image"},
			{`name="twitter:title"`, title},
			{`name="twitter:description"`, seo.Description},
			{`name="twitter:image"`, ogImage},
			{`name="twitter:image:alt"`, siteName + " logo"},
		}
		for _, m := range og {
			h = setTag(h, m.attr, "", `<meta `+m.attr+` content="`+html.EscapeString(m.content)+`">`)
		}
	}
	return h
}

// baseURLOrEmpty returns the configured web base URL or "" if not set. Used
// to build absolute asset URLs in the head (OG image etc.).
func (s *Server) baseURLOrEmpty() string {
	return s.Config.Web.BaseURL
}

// setTag replaces the first tag containing needle with replacement. When
// endNeedle is non-empty the replacement spans up to and including it (for
// <title>...</title>); otherwise it spans to the tag's closing '>'. If no
// matching tag exists the replacement is inserted just after endNeedle (when
// supplied) or before </head> as a fallback.
func setTag(h, needle, endNeedle, replacement string) string {
	start := strings.Index(h, needle)
	if start >= 0 {
		open := strings.LastIndexByte(h[:start], '<')
		if open >= 0 {
			end := 0
			if endNeedle != "" {
				if e := strings.Index(h[open:], endNeedle); e >= 0 {
					end = open + e + len(endNeedle)
				}
			} else if c := strings.IndexByte(h[start:], '>'); c >= 0 {
				end = start + c + 1
			}
			if end > open {
				return h[:open] + replacement + h[end:]
			}
		}
	}
	if endNeedle != "" {
		if i := strings.Index(h, endNeedle); i >= 0 {
			insertAt := i + len(endNeedle)
			return h[:insertAt] + replacement + h[insertAt:]
		}
	}
	if i := strings.Index(h, "</head>"); i >= 0 {
		return h[:i] + replacement + h[i:]
	}
	return h + replacement
}

// insertAfterTag inserts content immediately after the first occurrence of
// anchor. The anchor itself is preserved (this is the difference from
// setTag, which replaces). Falls back to inserting before </head> if the
// anchor is not present, and to appending if </head> is also missing.
func insertAfterTag(h, anchor, content string) string {
	if i := strings.Index(h, anchor); i >= 0 {
		insertAt := i + len(anchor)
		return h[:insertAt] + content + h[insertAt:]
	}
	if i := strings.Index(h, "</head>"); i >= 0 {
		return h[:i] + content + h[i:]
	}
	return h + content
}

// removeTag drops the first tag containing needle, if any.
func removeTag(h, needle string) string {
	start := strings.Index(h, needle)
	if start < 0 {
		return h
	}
	open := strings.LastIndexByte(h[:start], '<')
	if open < 0 {
		return h
	}
	if c := strings.IndexByte(h[start:], '>'); c >= 0 {
		return h[:open] + h[start+c+1:]
	}
	return h
}

// removeAllTags drops every tag containing needle. The shell may carry
// several favicon / theme-color tags (one per format); the injector
// replaces them as a group so the head never ends up with duplicates.
func removeAllTags(h, needle string) string {
	for {
		next := removeTag(h, needle)
		if next == h {
			return h
		}
		h = next
	}
}

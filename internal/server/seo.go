package server

import (
	"html"
	"net/http"
	"strings"
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
		site = "xListman"
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
		site = "xListman"
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
// route is gated, and Open Graph / canonical tags for public routes. Tags
// already present in the shell are replaced; missing ones are inserted just
// before </head>.
func injectHead(h string, siteName string, seo seoTags) string {
	title := seo.Title
	if title == "" {
		title = siteName
	}
	h = setTag(h, "<title", "</title>", "<title>"+html.EscapeString(title)+"</title>")
	if seo.Description != "" {
		h = setTag(h, `name="description"`, "", `<meta name="description" content="`+html.EscapeString(seo.Description)+`">`)
	}
	h = setTag(h, `name="xlistman-site-name"`, "", `<meta name="xlistman-site-name" content="`+html.EscapeString(siteName)+`">`)
	if seo.NoIndex {
		h = setTag(h, `name="robots"`, "", `<meta name="robots" content="noindex">`)
	} else {
		h = removeTag(h, `name="robots"`)
	}
	if seo.Canonical != "" {
		h = setTag(h, `rel="canonical"`, "", `<link rel="canonical" href="`+html.EscapeString(seo.Canonical)+`">`)
		og := []struct{ attr, content string }{
			{`property="og:title"`, title},
			{`property="og:description"`, seo.Description},
			{`property="og:type"`, "website"},
			{`property="og:url"`, seo.Canonical},
			{`property="og:site_name"`, siteName},
			{`name="twitter:card"`, "summary"},
			{`name="twitter:title"`, title},
			{`name="twitter:description"`, seo.Description},
		}
		for _, m := range og {
			h = setTag(h, m.attr, "", `<meta `+m.attr+` content="`+html.EscapeString(m.content)+`">`)
		}
	}
	return h
}

// setTag replaces the first tag containing needle with replacement. When
// endNeedle is non-empty the replacement spans up to and including it (for
// <title>...</title>); otherwise it spans to the tag's closing '>'. If no
// matching tag exists the replacement is inserted before </head>.
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
	if i := strings.Index(h, "</head>"); i >= 0 {
		return h[:i] + replacement + h[i:]
	}
	return h + replacement
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

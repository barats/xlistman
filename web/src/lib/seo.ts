// Per-page browser titles, meta descriptions, and indexing hints.
//
// The app is a pure SPA (ADR 0007), so the head is managed on the client. The
// Go server pre-injects a per-route title/description/site-name into the served
// shell; the SPA refines it on navigation via setSeo(). The configured site
// name (web.site_name) is read from the shell meta injected by the server.

const BRAND = 'xListman';

let siteName: string | null = null;

/** The configured instance name (web.site_name), or the default brand. */
export function getSiteName(): string {
	if (siteName === null) {
		const el = document.head.querySelector('meta[name="xlistman-site-name"]');
		const fromMeta = (el?.getAttribute('content') ?? '').trim();
		siteName = fromMeta || BRAND;
	}
	return siteName;
}

export interface SeoOptions {
	title: string;
	description: string;
	/** When true, adds robots noindex so the page stays out of search results. */
	noindex?: boolean;
}

/** Sets the browser tab title and meta description, and toggles noindex. */
export function setSeo(o: SeoOptions) {
	document.title = o.title;
	upsertMeta('description', o.description);
	if (o.noindex) {
		upsertMeta('robots', 'noindex');
	} else {
		removeMeta('robots');
	}
}

function upsertMeta(name: string, content: string) {
	let el = document.head.querySelector(`meta[name="${name}"]`);
	if (!el) {
		el = document.createElement('meta');
		el.setAttribute('name', name);
		document.head.appendChild(el);
	}
	el.setAttribute('content', content);
}

function removeMeta(name: string) {
	document.head.querySelector(`meta[name="${name}"]`)?.remove();
}

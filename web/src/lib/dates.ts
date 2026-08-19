// Date formatting shared across the app. fmtDate is a compact absolute
// timestamp (no seconds) for accountability surfaces like the audit trail;
// fmtRelative is a short relative time ("2h ago") for operational surfaces
// like moderation, falling back to the absolute form once something is old.

function parse(iso: string): Date | null {
	const d = new Date(iso);
	return Number.isNaN(d.getTime()) ? null : d;
}

export function fmtDate(iso: string): string {
	const d = parse(iso);
	if (!d) return iso;
	return d.toLocaleString(undefined, {
		year: 'numeric',
		month: '2-digit',
		day: '2-digit',
		hour: '2-digit',
		minute: '2-digit'
	});
}

export function fmtRelative(iso: string): string {
	const d = parse(iso);
	if (!d) return iso;
	const diff = Date.now() - d.getTime();
	if (diff < 0) return fmtDate(iso); // future timestamp (e.g. expiry)
	const mins = Math.round(diff / 60000);
	if (mins < 1) return 'just now';
	if (mins < 60) return `${mins}m ago`;
	const hours = Math.round(mins / 60);
	if (hours < 24) return `${hours}h ago`;
	const days = Math.round(hours / 24);
	if (days < 8) return `${days}d ago`;
	return fmtDate(iso);
}

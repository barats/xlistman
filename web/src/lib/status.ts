// Shared mapping from a Subscription Status (CONTEXT.md) to a badge variant.
// Active — the dominant state in dense member lists — stays neutral so the
// anomalous states are the ones that catch the eye: held (warning/amber),
// disabled (destructive/red), pending (outline).
export type StatusVariant =
	| 'default'
	| 'secondary'
	| 'destructive'
	| 'outline'
	| 'success'
	| 'warning';

export function statusVariant(status: string): StatusVariant {
	switch (status) {
		case 'active':
			return 'secondary';
		case 'held':
			return 'warning';
		case 'disabled':
			return 'destructive';
		default:
			return 'outline'; // pending
	}
}

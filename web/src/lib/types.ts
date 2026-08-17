export interface ListSummary {
	address: string;
	list_name: string;
	domain: string;
	description: string;
	list_type: string;
}

export interface ListInfo {
	address: string;
	list_name: string;
	domain: string;
	description: string;
	list_type: string;
	subscription_policy: string;
	moderation_enabled: boolean;
	digest_frequency: string;
}

export interface Subscription {
	id: number;
	address: string;
	list_name: string;
	domain: string;
	list_type: string;
	status: string;
	delivery_mode: string;
	bounce_count: number;
}

export interface Me {
	email: string;
	subscriptions: Subscription[];
}

export interface ArchiveEntry {
	id: number;
	subject: string;
	from: string;
	message_id: string;
	thread_id: string;
	received_at: string;
}

export interface ArchiveMessage extends ArchiveEntry {
	body: string;
}

export interface ConsoleList {
	address: string;
	list_name: string;
	domain: string;
	list_type: string;
	roles: string[];
	held_count: number;
}

export interface ConsoleListInfo {
	address: string;
	list_name: string;
	domain: string;
	list_type: string;
	roles: string[];
}

export interface HeldMessage {
	id: number;
	subject: string;
	sender: string;
	received_at: string;
	expires_at: string;
}

export interface HeldMessageDetail extends HeldMessage {
	body: string;
}

export interface DesignatedSender {
	id: number;
	email: string;
}

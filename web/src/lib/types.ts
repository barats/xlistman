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
	is_administrator: boolean;
	has_list_role: boolean;
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
	description: string;
	roles: string[];
}

export interface ListSettings {
	moderation_enabled: boolean;
	subject_prefix: string;
	footer_enabled: boolean;
	max_message_size: number;
	archive_max_age_days: number;
	digest_frequency: string;
	subscription_policy: string;
	reply_to_mode: string;
	reply_to_address: string;
	welcome_email: boolean;
	goodbye_email: boolean;
	sender_held_notice: boolean;
	owner_auto_disable_notice: boolean;
	bounce_threshold: number;
	held_expiry_days: number;
}

export interface ConsoleSettings {
	description: string;
	list_type: string;
	settings: ListSettings;
}

export interface ConsoleMember {
	subscriber_id: number;
	email: string;
	subscription_id?: number;
	status?: string;
	delivery_mode?: string;
	bounce_count: number;
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

// --- Server administration (ADR 0017) ---

export interface AdminInfo {
	is_administrator: boolean;
}

export interface AdminDomain {
	id: number;
	name: string;
	description: string;
	list_count: number;
}

export interface AdminList {
	address: string;
	list_name: string;
	domain: string;
	list_type: string;
	description: string;
	member_count: number;
}

export interface AdminAdministrator {
	id: number;
	email: string;
}

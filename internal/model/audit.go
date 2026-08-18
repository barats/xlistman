package model

import "time"

// AuditActorKind identifies who performed an audited action: a Subscriber
// (through the web or email paths) or the local CLI operator.
type AuditActorKind string

const (
	AuditActorSubscriber AuditActorKind = "subscriber"
	AuditActorCLI        AuditActorKind = "cli"
)

// Canonical Audit Event action names (ADR 0018). Namespaced so the two
// overloaded "approve" actions stay unambiguous.
const (
	ActionModerationApprove   = "moderation.approve"
	ActionModerationReject    = "moderation.reject"
	ActionModerationDiscard   = "moderation.discard"
	ActionSubscriptionApprove = "subscription.approve"
	ActionSubscriptionReject  = "subscription.reject"
	ActionMemberAdd           = "member.add"
	ActionMemberRemove        = "member.remove"
	ActionMemberReenable      = "member.re-enable"
	ActionMemberResetBounces  = "member.reset-bounces"
	ActionRoleGrant           = "role.grant"
	ActionRoleRevoke          = "role.revoke"
	ActionSenderAdd           = "sender.add"
	ActionSenderRemove        = "sender.remove"
	ActionSettingsUpdate      = "settings.update"
	ActionListCreate          = "list.create"
	ActionListDelete          = "list.delete"
	ActionListType            = "list.type"
	ActionDomainCreate        = "domain.create"
	ActionDomainDelete        = "domain.delete"
	ActionAdminDesignate      = "admin.designate"
	ActionAdminRevoke         = "admin.revoke"
	ActionWebLoginEnable      = "web.login-enable"
	ActionWebLoginDisable     = "web.login-disable"
	ActionWebManagementEnable = "web.management-enable"
	ActionWebManagementDisable = "web.management-disable"
)

// AuditActor identifies who performed an audited action. For a Subscriber it
// snapshots the id and email at the time of the action (ADR 0018); for the CLI
// it records the operator's OS user in Detail.
type AuditActor struct {
	Kind   AuditActorKind
	ID     int64
	Email  string
	Detail string
}

// AuditEvent is an immutable record of a privileged action (CONTEXT.md). It is
// never edited or deleted, and is not removed when the List it refers to is
// deleted. Canonical action names are namespaced (moderation.approve,
// subscription.approve, member.add, role.grant, sender.add, settings.update,
// list.create, domain.create, admin.designate) so the overloaded "approve"
// actions remain unambiguous.
type AuditEvent struct {
	ID          int64     `gorm:"primaryKey;autoIncrement"`
	At          time.Time `gorm:"not null;index"`
	ListID      *int64    `gorm:"index"`                    // nil for instance-level events
	ListAddr    string    `gorm:"not null;default:''"`      // snapshot of the list address
	Action      string    `gorm:"not null;index"`
	ActorKind   string    `gorm:"not null"`
	ActorID     *int64    // subscriber id when ActorKind is subscriber
	ActorEmail  string    `gorm:"not null;default:''"` // snapshot email ("" for CLI)
	ActorDetail string    `gorm:"not null;default:''"` // CLI operator detail (OS user)
	Target      string    `gorm:"not null;default:''"`
	Detail      string    `gorm:"not null;default:''"`
}

// NewAuditEvent builds an AuditEvent with its timestamp from an actor and a
// list snapshot. listID and listAddr are empty for instance-level events.
func NewAuditEvent(listID *int64, listAddr, action string, actor AuditActor, target, detail string) AuditEvent {
	e := AuditEvent{
		At:          time.Now(),
		ListID:      listID,
		ListAddr:    listAddr,
		Action:      action,
		ActorKind:   string(actor.Kind),
		ActorDetail: actor.Detail,
		Target:      target,
		Detail:      detail,
		ActorEmail:  actor.Email,
	}
	if actor.Kind == AuditActorSubscriber {
		id := actor.ID
		e.ActorID = &id
	}
	return e
}

// ActorLabel returns a stable display string for the event's actor.
func (e AuditEvent) ActorLabel() string {
	if e.ActorKind == string(AuditActorCLI) {
		return "CLI operator"
	}
	return e.ActorEmail
}

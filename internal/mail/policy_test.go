package mail

import (
	"testing"

	"github.com/barat/xlistman/internal/model"
)

func testList(listType model.ListType, moderation bool) model.List {
	return model.List{
		ListName: "dev",
		Domain:   "example.com",
		ListType: listType,
		Settings: model.ListSettings{
			ModerationEnabled: moderation,
		},
	}
}

func TestDecidePostAction_Discussion(t *testing.T) {
	l := testList(model.ListTypeDiscussion, false)

	if got := DecidePostAction(l, true, false, false); got != PostActionAccept {
		t.Errorf("subscriber post: got %v, want Accept", got)
	}
	if got := DecidePostAction(l, false, false, false); got != PostActionReject {
		t.Errorf("non-subscriber post: got %v, want Reject", got)
	}
}

func TestDecidePostAction_DiscussionModerated(t *testing.T) {
	l := testList(model.ListTypeDiscussion, true)

	if got := DecidePostAction(l, true, false, false); got != PostActionHold {
		t.Errorf("subscriber post (moderated): got %v, want Hold", got)
	}
	if got := DecidePostAction(l, false, false, false); got != PostActionHold {
		t.Errorf("non-subscriber post (moderated): got %v, want Hold", got)
	}
}

func TestDecidePostAction_Newsletter(t *testing.T) {
	l := testList(model.ListTypeNewsletter, false)

	if got := DecidePostAction(l, false, true, false); got != PostActionAccept {
		t.Errorf("owner post: got %v, want Accept", got)
	}
	if got := DecidePostAction(l, false, false, true); got != PostActionAccept {
		t.Errorf("designated sender post: got %v, want Accept", got)
	}
	if got := DecidePostAction(l, true, false, false); got != PostActionReject {
		t.Errorf("subscriber post to newsletter: got %v, want Reject", got)
	}
	if got := DecidePostAction(l, false, false, false); got != PostActionReject {
		t.Errorf("random post to newsletter: got %v, want Reject", got)
	}
}

package security

import "testing"

func TestACLManager_SetAndGet(t *testing.T) {
	am := NewACLManager()

	acl := &ChannelACL{
		Channel:     "room-a",
		Public:      false,
		MaxMembers:  10,
		AllowedKeys: []string{"key1", "key2"},
	}
	am.SetChannelACL(acl)

	got := am.GetChannelACL("room-a")
	if got == nil {
		t.Fatal("expected ACL for room-a")
	}
	if got.MaxMembers != 10 {
		t.Errorf("expected MaxMembers 10, got %d", got.MaxMembers)
	}

	// Non-existent channel
	got = am.GetChannelACL("room-b")
	if got != nil {
		t.Error("expected nil for non-existent channel")
	}
}

func TestACLManager_RemoveChannelACL(t *testing.T) {
	am := NewACLManager()
	am.SetChannelACL(&ChannelACL{Channel: "room-a", Public: false})

	ok := am.RemoveChannelACL("room-a")
	if !ok {
		t.Error("should return true for existing channel")
	}

	ok = am.RemoveChannelACL("room-a")
	if ok {
		t.Error("should return false for already-removed channel")
	}
}

func TestACLManager_ListACLs(t *testing.T) {
	am := NewACLManager()
	am.SetChannelACL(&ChannelACL{Channel: "room-a"})
	am.SetChannelACL(&ChannelACL{Channel: "room-b"})

	list := am.ListACLs()
	if len(list) != 2 {
		t.Errorf("expected 2 ACLs, got %d", len(list))
	}
}

func TestACLManager_CanJoin(t *testing.T) {
	am := NewACLManager()

	// No ACL = public = allowed
	if !am.CanJoin("room-x", "anykey", "anyuser") {
		t.Error("no ACL should allow join")
	}

	// Public channel
	am.SetChannelACL(&ChannelACL{Channel: "public-room", Public: true})
	if !am.CanJoin("public-room", "anykey", "anyuser") {
		t.Error("public channel should allow join")
	}

	// Restricted by API key
	am.SetChannelACL(&ChannelACL{
		Channel:     "private-room",
		Public:      false,
		AllowedKeys: []string{"key1", "key2"},
	})
	if !am.CanJoin("private-room", "key1", "user1") {
		t.Error("allowed key should join")
	}
	if am.CanJoin("private-room", "key3", "user1") {
		t.Error("non-allowed key should be rejected")
	}

	// Restricted by user permissions
	am.SetChannelACL(&ChannelACL{
		Channel:     "perm-room",
		Public:      false,
		Permissions: map[string]string{"user1": "write", "user2": "read"},
	})
	if !am.CanJoin("perm-room", "anykey", "user1") {
		t.Error("user with permission should join")
	}
	if am.CanJoin("perm-room", "anykey", "user3") {
		t.Error("user without permission should be rejected")
	}
}

func TestACLManager_CanWrite(t *testing.T) {
	am := NewACLManager()

	// No ACL = allowed
	if !am.CanWrite("room-x", "anyuser") {
		t.Error("no ACL should allow write")
	}

	// Public channel
	am.SetChannelACL(&ChannelACL{Channel: "public", Public: true})
	if !am.CanWrite("public", "anyuser") {
		t.Error("public channel should allow write")
	}

	// No permissions map = allowed
	am.SetChannelACL(&ChannelACL{Channel: "open", Public: false})
	if !am.CanWrite("open", "anyuser") {
		t.Error("no permissions map should allow write")
	}

	// Restricted
	am.SetChannelACL(&ChannelACL{
		Channel:     "restricted",
		Public:      false,
		Permissions: map[string]string{"user1": "write", "user2": "read"},
	})
	if !am.CanWrite("restricted", "user1") {
		t.Error("user with write should be allowed")
	}
	if am.CanWrite("restricted", "user2") {
		t.Error("user with only read should be rejected")
	}
	if am.CanWrite("restricted", "user3") {
		t.Error("user without any permission should be rejected")
	}
}

func TestACLManager_CheckMaxMembers(t *testing.T) {
	am := NewACLManager()

	// No ACL = no limit
	if !am.CheckMaxMembers("room-x", 9999) {
		t.Error("no ACL should allow any count")
	}

	// MaxMembers = 0 means no limit
	am.SetChannelACL(&ChannelACL{Channel: "unlimited", MaxMembers: 0})
	if !am.CheckMaxMembers("unlimited", 9999) {
		t.Error("MaxMembers 0 should have no limit")
	}

	// Actual limit
	am.SetChannelACL(&ChannelACL{Channel: "limited", MaxMembers: 5})
	if !am.CheckMaxMembers("limited", 4) {
		t.Error("4 < 5 should be allowed")
	}
	if am.CheckMaxMembers("limited", 5) {
		t.Error("5 >= 5 should be rejected")
	}
	if am.CheckMaxMembers("limited", 10) {
		t.Error("10 >= 5 should be rejected")
	}
}

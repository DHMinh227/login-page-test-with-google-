package session

import (
	"testing"
	"time"
)

func TestSessionLifecycle(t *testing.T) {
	store := NewStore(time.Hour)
	user := User{ID: "google-subject", Email: "player@example.com", Name: "Player"}

	token, _, err := store.Create(user)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Fatal("Create returned an empty token")
	}

	got, err := store.Get(token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != user {
		t.Fatalf("Get returned %#v; want %#v", got, user)
	}

	store.Delete(token)
	if _, err := store.Get(token); err != ErrInvalidSession {
		t.Fatalf("Get after Delete returned %v; want ErrInvalidSession", err)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	store := NewStore(time.Minute)
	base := time.Unix(1_700_000_000, 0)
	store.now = func() time.Time { return base }
	token, _, err := store.Create(User{ID: "subject"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	store.now = func() time.Time { return base.Add(2 * time.Minute) }
	if _, err := store.Get(token); err != ErrInvalidSession {
		t.Fatalf("Get expired session returned %v; want ErrInvalidSession", err)
	}
}

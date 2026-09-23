package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var ErrInvalidSession = errors.New("invalid or expired session")

type User struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"`
}

type entry struct {
	user      User
	expiresAt time.Time
}

type Store struct {
	mu       sync.RWMutex
	sessions map[[32]byte]entry
	lifetime time.Duration
	now      func() time.Time
}

func NewStore(lifetime time.Duration) *Store {
	return &Store{
		sessions: make(map[[32]byte]entry),
		lifetime: lifetime,
		now:      time.Now,
	}
}

func (s *Store) Create(user User) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := s.now().Add(s.lifetime)

	s.mu.Lock()
	s.sessions[sha256.Sum256([]byte(token))] = entry{user: user, expiresAt: expiresAt}
	s.mu.Unlock()

	return token, expiresAt, nil
}

func (s *Store) Get(token string) (User, error) {
	if token == "" {
		return User{}, ErrInvalidSession
	}
	key := sha256.Sum256([]byte(token))

	s.mu.RLock()
	item, ok := s.sessions[key]
	s.mu.RUnlock()
	if !ok || !s.now().Before(item.expiresAt) {
		if ok {
			s.Delete(token)
		}
		return User{}, ErrInvalidSession
	}
	return item.user, nil
}

func (s *Store) Delete(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, sha256.Sum256([]byte(token)))
	s.mu.Unlock()
}

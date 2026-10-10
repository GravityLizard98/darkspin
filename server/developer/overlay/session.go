package overlay

import (
	"crypto/sha256"
	"sync"
)

// keyLength is the hex length of the 32-byte key Fang generates per process.
const keyLength = 64

// sessionBinding ties one key digest to an account and its login token.
type sessionBinding struct {
	accountID     int64
	sessionDigest [32]byte
}

// sessionStore holds key bindings in memory only, so a server restart ends
// every binding. Keys are stored as SHA-256 digests.
type sessionStore struct {
	mutex                 sync.Mutex
	bindingsByKeyDigest   map[[32]byte]sessionBinding
	keyDigestsByAccountID map[int64][32]byte
}

func newSessionStore() *sessionStore {
	return &sessionStore{
		bindingsByKeyDigest:   make(map[[32]byte]sessionBinding),
		keyDigestsByAccountID: make(map[int64][32]byte),
	}
}

// bind stores a binding and replaces any earlier key of the same account.
func (e *sessionStore) bind(keyDigest [32]byte, binding sessionBinding) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	previousDigest, isPreviousFound := e.keyDigestsByAccountID[binding.accountID]
	if isPreviousFound {
		delete(e.bindingsByKeyDigest, previousDigest)
	}
	previousBinding, isKeyFound := e.bindingsByKeyDigest[keyDigest]
	if isKeyFound && previousBinding.accountID != binding.accountID {
		delete(e.keyDigestsByAccountID, previousBinding.accountID)
	}
	e.bindingsByKeyDigest[keyDigest] = binding
	e.keyDigestsByAccountID[binding.accountID] = keyDigest
}

func (e *sessionStore) lookup(keyDigest [32]byte) (sessionBinding, bool) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	binding, isFound := e.bindingsByKeyDigest[keyDigest]
	return binding, isFound
}

// remove deletes a binding only while it is still the one that was read, so
// a concurrent rebind of the account survives.
func (e *sessionStore) remove(keyDigest [32]byte, binding sessionBinding) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	current, isFound := e.bindingsByKeyDigest[keyDigest]
	if !isFound || current != binding {
		return
	}
	delete(e.bindingsByKeyDigest, keyDigest)
	if e.keyDigestsByAccountID[binding.accountID] == keyDigest {
		delete(e.keyDigestsByAccountID, binding.accountID)
	}
}

// isValidKey accepts exactly 64 lowercase hexadecimal characters.
func isValidKey(key string) bool {
	if len(key) != keyLength {
		return false
	}
	for index := 0; index < len(key); index++ {
		character := key[index]
		if character >= '0' && character <= '9' || character >= 'a' && character <= 'f' {
			continue
		}
		return false
	}
	return true
}

func digestKey(key string) [32]byte {
	return sha256.Sum256([]byte(key))
}

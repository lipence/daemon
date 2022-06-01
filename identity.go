package daemon

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/atomic"
)

var ErrIdentityAlreadyExists = fmt.Errorf("identity already exists")

type Identity struct {
	name string
	uuid uuid.UUID
}

func (i *Identity) String() string { return i.name }

func (i *Identity) Name() string { return i.name }

func (i *Identity) UUID() uuid.UUID { return i.uuid }

type identityRegisterType struct {
	lock   sync.RWMutex
	pool   []Identity
	byName map[string]int
	byUUID map[uuid.UUID]int
}

func (r *identityRegisterType) Register(name string, uid uuid.UUID) (id *Identity, err error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if _, ok := r.byUUID[uid]; ok {
		return nil, ErrIdentityAlreadyExists
	}
	if _, ok := r.byName[name]; ok {
		return nil, ErrIdentityAlreadyExists
	}
	return r.append(name, uid), nil
}

func (r *identityRegisterType) Generate(name string) (id *Identity, err error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	if _, ok := r.byName[name]; ok {
		return nil, ErrIdentityAlreadyExists
	}
	var uid uuid.UUID
	for {
		uid = uuid.New()
		if _, ok := r.byUUID[uid]; !ok {
			break
		}
	}
	return r.append(name, uid), nil
}

func (r *identityRegisterType) ByUUID(uid uuid.UUID) (*Identity, bool) {
	r.lock.RLock()
	defer r.lock.RUnlock()
	if i, ok := r.byUUID[uid]; !ok {
		return nil, false
	} else {
		return &r.pool[i], true
	}
}

func (r *identityRegisterType) ByName(name string) (*Identity, bool) {
	r.lock.RLock()
	defer r.lock.RUnlock()
	if i, ok := r.byName[name]; !ok {
		return nil, false
	} else {
		return &r.pool[i], true
	}
}

func (r *identityRegisterType) append(name string, uid uuid.UUID) *Identity {
	var newPool = append(r.pool, Identity{uuid: uid, name: name})
	var newIdIndex = len(newPool) - 1
	r.pool, r.byName[name], r.byUUID[uid] = newPool, newIdIndex, newIdIndex
	return &newPool[newIdIndex]
}

var identityRegisterCreated = atomic.NewBool(false)
var identityRegisterGlobal *identityRegisterType

func Identities() *identityRegisterType {
	if identityRegisterCreated.CAS(false, true) {
		identityRegisterGlobal = &identityRegisterType{
			byName: map[string]int{},
			byUUID: map[uuid.UUID]int{},
		}
	}
	return identityRegisterGlobal
}

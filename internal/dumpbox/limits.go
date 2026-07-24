package dumpbox

import "sync"

type uploadLimits struct {
	requestBytes    int64
	fileBytes       int64
	filesPerRequest int
}

// uploadSlots caps how many uploads a single subject and the whole process may
// stream concurrently, so that slow requests cannot exhaust connections,
// goroutines, or file descriptors.
type uploadSlots struct {
	mutex    sync.Mutex
	perUser  int
	total    int
	active   map[string]int
	inFlight int
}

func newUploadSlots(perUser, total int) *uploadSlots {
	return &uploadSlots{perUser: perUser, total: total, active: make(map[string]int)}
}

func (u *uploadSlots) acquire(subject string) bool {
	if u == nil {
		return true
	}
	u.mutex.Lock()
	defer u.mutex.Unlock()
	if u.inFlight >= u.total || u.active[subject] >= u.perUser {
		return false
	}
	u.inFlight++
	u.active[subject]++
	return true
}

func (u *uploadSlots) release(subject string) {
	if u == nil {
		return
	}
	u.mutex.Lock()
	defer u.mutex.Unlock()
	if u.active[subject] <= 1 {
		delete(u.active, subject)
	} else {
		u.active[subject]--
	}
	if u.inFlight > 0 {
		u.inFlight--
	}
}

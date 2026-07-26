package dumpbox

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

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

type storageQuota struct {
	mutex sync.Mutex
	max   int64
	used  map[string]int64
}

func newStorageQuota(dataDir string, max int64) (*storageQuota, error) {
	quota := &storageQuota{max: max, used: make(map[string]int64)}
	if max == 0 {
		return quota, nil
	}
	entries, err := os.ReadDir(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return quota, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		key, ok := quotaKeyFromDirectory(entry.Name())
		if !ok || !entry.IsDir() {
			continue
		}
		err := filepath.WalkDir(filepath.Join(dataDir, entry.Name()), func(_ string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !item.IsDir() {
				info, err := item.Info()
				if err != nil {
					return err
				}
				if info.Mode().IsRegular() {
					quota.used[key] += info.Size()
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return quota, nil
}

func quotaKey(subject string) string {
	return sha256Sum(subject)[:24]
}

func quotaKeyFromDirectory(name string) (string, bool) {
	if !strings.HasPrefix(name, "user-") {
		return "", false
	}
	key := name[len(name)-min(len(name), 24):]
	if len(key) != 24 {
		return "", false
	}
	for _, character := range key {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return "", false
		}
	}
	return key, true
}

func (q *storageQuota) reserve(subject string, bytes int64) bool {
	if q == nil || q.max == 0 {
		return true
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	key := quotaKey(subject)
	if bytes > q.max-q.used[key] {
		return false
	}
	q.used[key] += bytes
	return true
}

func (q *storageQuota) release(subject string, bytes int64) {
	if q == nil || q.max == 0 {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	key := quotaKey(subject)
	q.used[key] -= bytes
	if q.used[key] <= 0 {
		delete(q.used, key)
	}
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

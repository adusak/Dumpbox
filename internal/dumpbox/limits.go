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
	mutex    sync.Mutex
	maxBytes int64
	maxFiles int
	bytes    map[string]int64
	files    map[string]int
}

func newStorageQuota(dataDir string, maxBytes int64, maxFiles int) (*storageQuota, error) {
	quota := &storageQuota{
		maxBytes: maxBytes,
		maxFiles: maxFiles,
		bytes:    make(map[string]int64),
		files:    make(map[string]int),
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
					quota.bytes[key] += info.Size()
					quota.files[key]++
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

func (q *storageQuota) reserveBytes(subject string, bytes int64) bool {
	if q == nil || q.maxBytes == 0 {
		return true
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	key := quotaKey(subject)
	if bytes > q.maxBytes-q.bytes[key] {
		return false
	}
	q.bytes[key] += bytes
	return true
}

func (q *storageQuota) releaseBytes(subject string, bytes int64) {
	if q == nil || q.maxBytes == 0 {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	key := quotaKey(subject)
	q.bytes[key] -= bytes
	if q.bytes[key] <= 0 {
		delete(q.bytes, key)
	}
}

func (q *storageQuota) reserveFile(subject string) bool {
	if q == nil || q.maxFiles == 0 {
		return true
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	key := quotaKey(subject)
	if q.files[key] >= q.maxFiles {
		return false
	}
	q.files[key]++
	return true
}

func (q *storageQuota) releaseFile(subject string) {
	if q == nil || q.maxFiles == 0 {
		return
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	key := quotaKey(subject)
	q.files[key]--
	if q.files[key] <= 0 {
		delete(q.files, key)
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

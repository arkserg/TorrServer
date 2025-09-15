package settings

import (
	"errors"
	"strings"

	"server/log"

	bolt "go.etcd.io/bbolt"
)

func normalizeDLNAHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

func HasDLNATitleBucket(hashHex string) bool {
	if tdb == nil {
		return false
	}
	hashHex = normalizeDLNAHash(hashHex)
	if hashHex == "" {
		return false
	}
	exists, err := hasDLNATitleBucket(tdb, hashHex)
	if err != nil {
		log.TLogln("HasDLNATitleBucket: check failed", err)
	}
	return exists
}

func StoreDLNATitles(hashHex string, titles map[string]string) {
	if tdb == nil || ReadOnly {
		return
	}
	hashHex = normalizeDLNAHash(hashHex)
	if hashHex == "" || len(titles) == 0 {
		return
	}

	cleaned := make(map[string]string, len(titles))
	for path, title := range titles {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		cleaned[path] = title
	}
	if len(cleaned) == 0 {
		return
	}

	storeDLNATitles(tdb, hashHex, cleaned)
}

// Cache for DLNA normalized titles keyed by torrent hash
func GetDLNATitle(hashHex, path string) string {
	if tdb == nil {
		return ""
	}
	hashHex = normalizeDLNAHash(hashHex)
	if hashHex == "" || path == "" {
		return ""
	}
	buf := tdb.Get("DLNATitles/"+hashHex, path)
	if len(buf) == 0 {
		return ""
	}
	return string(buf)
}

func SetDLNATitle(hashHex, path, title string) {
	if tdb == nil {
		return
	}
	hashHex = normalizeDLNAHash(hashHex)
	if hashHex == "" || path == "" {
		return
	}
	tdb.Set("DLNATitles/"+hashHex, path, []byte(title))
}

func RemDLNATitles(hashHex string) {
	if tdb == nil {
		return
	}
	hashHex = normalizeDLNAHash(hashHex)
	if hashHex == "" {
		return
	}
	removeDLNATitleBucket(tdb, hashHex)
}

func removeDLNATitleBucket(db TorrServerDB, hashHex string) {
	switch v := db.(type) {
	case *DBReadCache:
		prefix := "DLNATitles/" + hashHex
		v.listCacheMutex.Lock()
		delete(v.listCache, prefix)
		v.listCacheMutex.Unlock()

		v.dataCacheMutex.Lock()
		for key := range v.dataCache {
			if key[0] == prefix {
				delete(v.dataCache, key)
			}
		}
		v.dataCacheMutex.Unlock()
		if v.db != nil {
			removeDLNATitleBucket(v.db, hashHex)
		}
	case *XPathDBRouter:
		if routed := v.getDBForXPath("DLNATitles/" + hashHex); routed != nil {
			removeDLNATitleBucket(routed, hashHex)
		}
	case *TDB:
		if err := v.deleteDLNATitleBucket(hashHex); err != nil {
			log.TLogln("removeDLNATitleBucket: delete bucket failed", err)
		}
	default:
		db.Rem("DLNATitles", hashHex)
	}
}

func hasDLNATitleBucket(db TorrServerDB, hashHex string) (bool, error) {
	switch v := db.(type) {
	case *DBReadCache:
		prefix := "DLNATitles/" + hashHex
		v.listCacheMutex.RLock()
		if _, ok := v.listCache[prefix]; ok {
			v.listCacheMutex.RUnlock()
			return true, nil
		}
		v.listCacheMutex.RUnlock()

		v.dataCacheMutex.RLock()
		for key := range v.dataCache {
			if key[0] == prefix {
				v.dataCacheMutex.RUnlock()
				return true, nil
			}
		}
		v.dataCacheMutex.RUnlock()

		if v.db != nil {
			return hasDLNATitleBucket(v.db, hashHex)
		}
		return false, nil
	case *XPathDBRouter:
		if routed := v.getDBForXPath("DLNATitles/" + hashHex); routed != nil {
			return hasDLNATitleBucket(routed, hashHex)
		}
		return false, nil
	case *TDB:
		return v.hasDLNATitleBucket(hashHex)
	default:
		names := db.List("DLNATitles/" + hashHex)
		return len(names) > 0, nil
	}
}

func storeDLNATitles(db TorrServerDB, hashHex string, titles map[string]string) {
	switch v := db.(type) {
	case *DBReadCache:
		prefix := "DLNATitles/" + hashHex
		v.listCacheMutex.Lock()
		delete(v.listCache, prefix)
		v.listCacheMutex.Unlock()

		v.dataCacheMutex.Lock()
		for key := range v.dataCache {
			if key[0] == prefix {
				delete(v.dataCache, key)
			}
		}
		v.dataCacheMutex.Unlock()

		if v.db != nil {
			storeDLNATitles(v.db, hashHex, titles)
		}
	case *XPathDBRouter:
		if routed := v.getDBForXPath("DLNATitles/" + hashHex); routed != nil {
			storeDLNATitles(routed, hashHex, titles)
		}
	case *TDB:
		if err := v.createDLNATitleBucket(hashHex, titles); err != nil {
			log.TLogln("storeDLNATitles: create bucket failed", err)
		}
	default:
		prefix := "DLNATitles/" + hashHex
		for path, title := range titles {
			db.Set(prefix, path, []byte(title))
		}
	}
}

func (v *TDB) deleteDLNATitleBucket(hashHex string) error {
	if v == nil || v.db == nil {
		return nil
	}
	return v.db.Update(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte("DLNATitles"))
		if root == nil {
			return nil
		}
		err := root.DeleteBucket([]byte(hashHex))
		if errors.Is(err, bolt.ErrBucketNotFound) {
			return nil
		}
		return err
	})
}

func (v *TDB) hasDLNATitleBucket(hashHex string) (bool, error) {
	if v == nil || v.db == nil {
		return false, nil
	}
	exists := false
	err := v.db.View(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte("DLNATitles"))
		if root == nil {
			return nil
		}
		if root.Bucket([]byte(hashHex)) != nil {
			exists = true
		}
		return nil
	})
	return exists, err
}

func (v *TDB) createDLNATitleBucket(hashHex string, titles map[string]string) error {
	if v == nil || v.db == nil {
		return nil
	}
	if len(titles) == 0 {
		return nil
	}
	return v.db.Update(func(tx *bolt.Tx) error {
		root, err := tx.CreateBucketIfNotExists([]byte("DLNATitles"))
		if err != nil {
			return err
		}
		if root.Bucket([]byte(hashHex)) != nil {
			return nil
		}
		bucket, err := root.CreateBucket([]byte(hashHex))
		if err != nil {
			if errors.Is(err, bolt.ErrBucketExists) {
				return nil
			}
			return err
		}
		for path, title := range titles {
			if err := bucket.Put([]byte(path), []byte(title)); err != nil {
				return err
			}
		}
		return nil
	})
}

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

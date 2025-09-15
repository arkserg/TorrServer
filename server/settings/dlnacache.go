package settings

import (
	"encoding/json"
	"strings"
)

type dlnatitleEntry struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

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
	buf := tdb.Get("DLNATitles", hashHex)
	if len(buf) == 0 {
		return ""
	}
	var entries []dlnatitleEntry
	if err := json.Unmarshal(buf, &entries); err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.Path == path {
			return entry.Title
		}
	}
	return ""
}

func SetDLNATitle(hashHex, path, title string) {
	if tdb == nil {
		return
	}
	hashHex = normalizeDLNAHash(hashHex)
	if hashHex == "" || path == "" {
		return
	}
	var entries []dlnatitleEntry
	if buf := tdb.Get("DLNATitles", hashHex); len(buf) > 0 {
		if err := json.Unmarshal(buf, &entries); err != nil {
			entries = nil
		}
	}
	updated := false
	for i := range entries {
		if entries[i].Path == path {
			entries[i].Title = title
			updated = true
			break
		}
	}
	if !updated {
		entries = append(entries, dlnatitleEntry{Path: path, Title: title})
	}
	if buf, err := json.Marshal(entries); err == nil {
		tdb.Set("DLNATitles", hashHex, buf)
	}
}

func RemDLNATitles(hashHex string) {
	if tdb == nil {
		return
	}
	hashHex = normalizeDLNAHash(hashHex)
	if hashHex == "" {
		return
	}
	tdb.Rem("DLNATitles", hashHex)
}

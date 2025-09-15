package torr

import (
	"server/dlnatitles"
	"server/log"
	mt "server/mimetype"
	"server/settings"
)

func ensureDLNATitles(t *Torrent) {
	if t == nil {
		return
	}
	hash := t.Hash().HexString()
	if hash == "" {
		return
	}

	status := t.Status()
	for _, file := range status.FileStats {
		if file == nil || file.Path == "" {
			continue
		}
		mime, err := mt.MimeTypeByPath(file.Path)
		if err != nil {
			if settings.BTsets.EnableDebug {
				log.TLogln("ensureDLNATitles: can't detect mime type", err)
			}
			continue
		}
		if !mime.IsMedia() {
			continue
		}
		dlnatitles.Ensure(hash, file.Path)
	}
}

// EnsureDLNATitles precomputes and stores normalized DLNA titles for torrent media files.
func (t *Torrent) EnsureDLNATitles() {
	ensureDLNATitles(t)
}

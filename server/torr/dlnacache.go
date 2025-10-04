package torr

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	var mediaPaths []string
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
		mediaPaths = append(mediaPaths, file.Path)
	}

	if len(mediaPaths) == 0 {
		return
	}

	dlnatitles.EnsureTorrent(hash, mediaPaths)
	createStreamLinkFiles(t, mediaPaths)
}

// EnsureDLNATitles precomputes and stores normalized DLNA titles for torrent media files.
func (t *Torrent) EnsureDLNATitles() {
	ensureDLNATitles(t)
}

func createStreamLinkFiles(t *Torrent, mediaPaths []string) {
	if t == nil || len(mediaPaths) == 0 {
		return
	}

	baseDir := streamLinksRoot()
	if baseDir == "" {
		return
	}

	hashHex := strings.ToLower(strings.TrimSpace(t.Hash().HexString()))
	if hashHex == "" {
		return
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		log.TLogln("ensureDLNATitles: can't prepare strmlnk root", err)
		return
	}

	removeStreamLinkDir(hashHex)

	dirName := sanitizeFileName(t.Title)
	if dirName == "" && t.Torrent != nil && t.Torrent.Info() != nil {
		dirName = sanitizeFileName(t.Info().Name)
	}
	if dirName == "" {
		dirName = hashHex
	}

	torrentDir := filepath.Join(baseDir, dirName)
	if err := os.MkdirAll(torrentDir, 0o755); err != nil {
		log.TLogln("ensureDLNATitles: can't create torrent strmlnk dir", err)
		return
	}

	allowed := make(map[string]struct{}, len(mediaPaths))
	for _, p := range mediaPaths {
		if p == "" {
			continue
		}
		allowed[p] = struct{}{}
	}

	baseURL := streamBaseURL()
	if baseURL == "" {
		_ = os.RemoveAll(torrentDir)
		return
	}

	status := t.Status()
	nameCounts := make(map[string]int)
	for _, file := range status.FileStats {
		if file == nil || file.Path == "" {
			continue
		}
		if _, ok := allowed[file.Path]; !ok {
			continue
		}

		title := strings.TrimSpace(dlnatitles.Lookup(hashHex, file.Path))
		if title == "" {
			title = filepath.Base(file.Path)
		}
		baseName := sanitizeFileName(title)
		if baseName == "" {
			baseName = fmt.Sprintf("file-%d", file.Id)
		}

		count := nameCounts[baseName]
		nameCounts[baseName] = count + 1

		name := baseName
		if count > 0 {
			name = fmt.Sprintf("%s (%d)", baseName, count+1)
		}

		link := buildStreamLink(baseURL, hashHex, file.Path, file.Id)
		filePath := filepath.Join(torrentDir, name+".strmlnk")
		if err := os.WriteFile(filePath, []byte(link), 0o644); err != nil {
			log.TLogln("ensureDLNATitles: can't write strmlnk", err)
		}
	}

	if err := os.WriteFile(filepath.Join(torrentDir, ".hash"), []byte(hashHex), 0o644); err != nil {
		log.TLogln("ensureDLNATitles: can't write hash marker", err)
	}
}

func streamLinksRoot() string {
	base := strings.TrimSpace(settings.Path)
	if base == "" {
		return ""
	}
	return filepath.Join(base, "dlnacache")
}

func streamBaseURL() string {
	host := defaultStreamHost()
	if host == "" {
		return ""
	}

	port := strings.TrimSpace(settings.Port)
	if port == "" {
		port = "8090"
	}

	return "http://" + net.JoinHostPort(host, port)
}

func buildStreamLink(baseURL, hashHex, path string, id int) string {
	if baseURL == "" || hashHex == "" || path == "" {
		return ""
	}
	name := filepath.Base(path)
	escaped := url.PathEscape(name)
	return fmt.Sprintf("%s/stream/%s?link=%s&index=%d&play", baseURL, escaped, hashHex, id)
}

func defaultStreamHost() string {
	if host := strings.TrimSpace(settings.PubIPv4); host != "" {
		return host
	}
	if host := strings.TrimSpace(settings.IP); host != "" && host != "0.0.0.0" && host != "::" && host != "[::]" {
		return host
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}

	var firstIPv6 string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				return v4.String()
			}
			if firstIPv6 == "" {
				firstIPv6 = ip.String()
			}
		}
	}

	if firstIPv6 != "" {
		return firstIPv6
	}

	return "127.0.0.1"
}

func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	var b strings.Builder
	for _, r := range name {
		if r < 32 || r == 127 {
			continue
		}
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
		if b.Len() >= 200 {
			break
		}
	}

	cleaned := strings.Trim(b.String(), " ._")
	return cleaned
}

func removeStreamLinkDir(hashHex string) {
	base := streamLinksRoot()
	if base == "" {
		return
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}

	target := strings.ToLower(strings.TrimSpace(hashHex))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		marker, err := os.ReadFile(filepath.Join(base, entry.Name(), ".hash"))
		if err != nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(string(marker))) == target {
			_ = os.RemoveAll(filepath.Join(base, entry.Name()))
		}
	}
}

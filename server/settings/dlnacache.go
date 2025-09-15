package settings

// Cache for DLNA normalized titles

func GetDLNATitle(path string) string {
	if tdb == nil {
		return ""
	}
	if buf := tdb.Get("DLNATitles", path); len(buf) > 0 {
		return string(buf)
	}
	return ""
}

func SetDLNATitle(path, title string) {
	if tdb == nil {
		return
	}
	tdb.Set("DLNATitles", path, []byte(title))
}

func RemDLNATitle(path string) {
	if tdb == nil {
		return
	}
	tdb.Rem("DLNATitles", path)
}

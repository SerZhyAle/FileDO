//go:build !windows

package main

import (
	"os"

	"filedo/fdsec"
)

func fdsecMetadataFromStat(fi os.FileInfo) fdsec.Metadata {
	return fdsec.Metadata{
		Name:       fi.Name(),
		Size:       fi.Size(),
		ModifiedAt: fi.ModTime(),
	}
}

func fdsecRestoreTimes(path string, m fdsec.Metadata) {
	_ = os.Chtimes(path, m.AccessedAt, m.ModifiedAt)
}

package vdisk

import "context"

// File-level extraction needs an NTFS reader. No permissively licensed one is
// vendored yet, and the package does not carry a half-written parser that
// could lose files quietly, so this build refuses the operation by name and
// the raw image remains the escape route (ExportRaw): the volume it produces
// opens in any tool that reads NTFS, Windows included.

// Selector chooses what ExportTree extracts; the empty Selector is the whole
// tree.
type Selector struct {
	Paths []string
}

// ExportReport is what ExportTree wrote.
type ExportReport struct {
	Files int64
	Bytes int64
}

// ExportTree extracts the file tree inside the volume into dest. Not in this
// build: it returns ErrUnsupported and writes nothing.
func (c *Container) ExportTree(ctx context.Context, dest string, sel Selector) (ExportReport, error) {
	return ExportReport{}, unsupportedf("file-level extraction is not in this build; export the raw image, which any NTFS reader opens")
}

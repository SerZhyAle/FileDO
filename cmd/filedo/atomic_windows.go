package main

import (
	"context"

	"filedo/fsx"
)

// Atomic, non-replacing writes (B2 of SP-0027, theme T4 of SP-0023) live in
// the shared package fsx. A copy writes `<name>.filedo-partial`, flushes it,
// sets its times and only then renames it into place - with no replace unless
// the user chose to overwrite that exact path - so a failure, a stop or a
// crash never leaves a truncated file under the final name.

const partialSuffix = fsx.PartialSuffix

var errDestinationExists = fsx.ErrDestinationExists

type partialFile = fsx.PartialFile

type atomicCopyOptions = fsx.CopyOptions

func isPartialName(name string) bool { return fsx.IsPartialName(name) }

func renameNoReplace(src, dst string) error { return fsx.RenameNoReplace(src, dst) }

func renameReplace(src, dst string) error { return fsx.RenameReplace(src, dst) }

func createPartial(dst string) (*partialFile, error) { return fsx.CreatePartial(dst) }

func atomicCopyFile(ctx context.Context, src, dst string, opt atomicCopyOptions) error {
	return fsx.CopyFile(ctx, src, dst, opt)
}

package main

import "filedo/fsx"

// Path identity (B1 of SP-0027, theme T2 of SP-0023) lives in the shared
// package fsx, because the duplicate finder needs the same answers; these
// names keep the call sites in this package short.

// PathIdentity is fsx.Identity: final path, volume, volume serial + file ID.
type PathIdentity = fsx.Identity

func pathIdentityOf(p string) (PathIdentity, error) { return fsx.IdentityOf(p) }

// sameFilePaths: do two existing paths name one object?
func sameFilePaths(a, b string) (bool, error) { return fsx.SameFile(a, b) }

// resolvedPath: the canonical spelling of p, even if p does not exist yet.
func resolvedPath(p string) (string, error) { return fsx.Resolve(p) }

// pathWithin: is child the parent itself or below it?
func pathWithin(child, parent string) (bool, error) { return fsx.Within(child, parent) }

// pathsOverlap: are a and b the same location, or does one contain the other?
func pathsOverlap(a, b string) (bool, error) { return fsx.Overlap(a, b) }

// overlapError is the one message for a refused overlapping source and target.
func overlapError(verb, source, target string) error { return fsx.OverlapError(verb, source, target) }

func canonicalWithin(child, parent string) bool { return fsx.CanonicalWithin(child, parent) }

func longPathForAPI(p string) string { return fsx.LongPath(p) }

func hasPrefixFold(s, prefix string) bool { return fsx.HasPrefixFold(s, prefix) }

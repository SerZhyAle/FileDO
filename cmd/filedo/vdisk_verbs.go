package main

import "strings"

// The vd verb table (SP-0004 spec 5.2, P6 T6.7): every verb, its words, and
// what it needs. It is the one place a word becomes a verb - vdClaims, runVd
// and the tests read it; the redaction keeps its own list (fdsec_redact.go),
// which TestVD_Aliases holds to this one.
//
// No word here may equal an operation word of the generic chain
// (isOperationWord) or an option word of the sibling (fdsecOptionWords):
// that is why format never shortens to f, compact never to c and save never
// to s. info is the one shared word, on purpose: `x.fdd info` answers from
// the container (spec 5.2).

var list_of_flags_for_vd = []string{"vd", "vdisk"}

var (
	vdNewWords     = []string{"new", "create"}
	vdMountWords   = []string{"mount", "mnt", "attach"}
	vdUnmountWords = []string{"unmount", "umount", "detach"}
	vdInfoWords    = []string{"info", "i"}
	vdVerifyWords  = []string{"verify", "vfy"}
	vdExportWords  = []string{"export", "extract", "ext"}
	vdSaveWords    = []string{"save"}
	vdCompactWords = []string{"compact", "shrink"}
	vdGrowWords    = []string{"grow", "resize"}
	vdFormatWords  = []string{"format"}
	vdSealWords    = []string{"seal"}
	vdCloneWords   = []string{"clone"}
	vdPassWords    = []string{"pass"}
	vdDestroyWords = []string{"destroy", "erase"}
)

// vdVerb is one verb of the table. container: it acts on an existing
// container, so it has both forms and its first word is resolved through the
// registry. transport: it needs the mount path - the block server, the
// initiator or elevation - which the packaged build does not carry (T6.26).
type vdVerb struct {
	name      string
	words     []string
	container bool
	transport bool
	judges    bool // it answers a question about its target (Passed) rather than acting on it (Done)
}

var vdVerbs = []vdVerb{
	{name: "new", words: vdNewWords},
	{name: "mount", words: vdMountWords, container: true, transport: true},
	{name: "unmount", words: vdUnmountWords, container: true, transport: true},
	{name: "info", words: vdInfoWords, container: true, judges: true},
	{name: "verify", words: vdVerifyWords, container: true, judges: true},
	{name: "export", words: vdExportWords, container: true},
	{name: "save", words: vdSaveWords, container: true, transport: true},
	{name: "compact", words: vdCompactWords, container: true},
	{name: "grow", words: vdGrowWords, container: true},
	{name: "format", words: vdFormatWords, container: true, transport: true},
	{name: "seal", words: vdSealWords, container: true},
	{name: "clone", words: vdCloneWords, container: true},
	{name: "pass", words: vdPassWords, container: true},
	{name: "destroy", words: vdDestroyWords, container: true},
	{name: "add", words: []string{"add"}},
	{name: "forget", words: []string{"forget"}},
	{name: "list", words: []string{"list", "ls"}, judges: true},
	{name: "auto", words: []string{"auto"}, transport: true},
	{name: "status", words: []string{"status"}, judges: true},
	// The Explorer registration of .fdd (T6.16, vdisk_register_windows.go).
	// Not a transport verb: inside the package it refuses with its own
	// sentence, which says where the Store build's .fdd type comes from.
	{name: "register", words: []string{"register"}},
	{name: "unregister", words: []string{"unregister"}},
	{name: "stop", words: []string{"stop"}},
}

// vdVerbOf returns the verb a word names, and false for a word that is none.
func vdVerbOf(word string) (vdVerb, bool) {
	w := strings.ToLower(word)
	for _, v := range vdVerbs {
		if contains(v.words, w) {
			return v, true
		}
	}
	return vdVerb{}, false
}

// vdVerbList is the verb list the usage messages name.
func vdVerbList() string {
	names := make([]string, 0, len(vdVerbs))
	for _, v := range vdVerbs {
		names = append(names, v.name)
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

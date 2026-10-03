package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"filedo/fdsec"
	"filedo/vdisk"
)

// The file verbs of a container (SP-0004 P6): verify, export, compact, grow
// and pass. None of them needs the transport or elevation, so all of them work
// in every build, the packaged one included (spec 5.8 item 2). Each opens the
// container itself, under the lock of FDD-BEHAVIOUR section 3: a reader shares
// it, a writer holds it alone, and a mounted container's block server holds it
// already - which is the second, structural reason a writer is refused while a
// container is mounted; the first is the explicit check below, which names the
// drive letter.

// vdRefuseMounted is busy, class 8, when the container is mounted: the verb
// needs it unmounted, and no option skips that.
func vdRefuseMounted(path string, info vdisk.Info, verb string) error {
	if m, ok := vdFindMount(info.ContainerID); ok {
		return errBusy(fmt.Sprintf("%s is mounted at %s; %s needs it unmounted first (filedo %s unmount), and no option skips that", path, m.Letter, verb, m.Letter))
	}
	return nil
}

// vdOfflineWriterGate is what grow and compact say before they write to a
// container that was not closed cleanly or whose ram save was cut (AUD-36-F1
// clause 2, FDD-BEHAVIOUR 5 rule 1): they name it first and ask, the way mount
// does; a batch never answers, so it refuses. The verb does not repair the
// volume, and the container keeps its marker (AUD-36-F2).
//
// vdOfflineWriterRefusal is the words of that refusal the window looks for
// (filedo_win_vb\DiskJobs.vb, DiskCommands.OfflineWriterRefusal): a batch run
// is class 2 for many reasons, and this one gets a sentence of its own.
const vdOfflineWriterRefusal = "was not closed cleanly and needs an answer"

func vdOfflineWriterGate(path string, info vdisk.Info, verb string, batch bool) error {
	switch {
	case info.SaveInProgress:
		fmt.Printf("WARNING: a save of this ram container was interrupted. It began %s; the last complete save was %s.\n", vdTime(info.SaveStarted), vdTime(info.LastGoodSave))
		fmt.Printf("The volume may hold a mixture of the two states, and %s does not repair it. Copy the file first if its contents matter.\n", verb)
	case !info.Clean:
		fmt.Printf("Note: %s was not closed cleanly the last time; last good save %s. %s does not repair the volume, and the container stays marked not closed cleanly.\n", path, vdTime(info.LastGoodSave), verb)
	default:
		return nil
	}
	if !vdConfirm(fmt.Sprintf("Run %s anyway?", verb), batch) {
		return vdUsagef("not changed: %s %s (run %s from a console, or mount it once so Windows checks the volume, unmount it, then try again)", path, vdOfflineWriterRefusal, verb)
	}
	return nil
}

// vdCredOnly reads the words of a verb whose only option is the credential.
func vdCredOnly(verb string, words []string) (credArg, error) {
	var cred credArg
	for i, w := range words {
		a, ok := credentialToken(w)
		if !ok || cred.given() {
			// Never quoted back: the word may be a password without its prefix.
			return credArg{}, vdUsagef("unknown word %d for %s: it takes at most one credential, as p:, pf:, pe: or k:", i+2, verb)
		}
		cred = a
	}
	return cred, nil
}

// vdForgetEnv takes a pe: variable out of the environment even when the verb
// did not need it, so nothing this process starts later inherits it.
func vdForgetEnv(a credArg) {
	if a.src == "pe" {
		fdsecLookupCredentialEnv(a.val)
	}
}

// vdCredentialFor is the credential one container needs: none for an
// obfuscated one (a given credential is not used, and said so), the resolved
// one for an encrypted one.
func vdCredentialFor(info vdisk.Info, a credArg) (fdsec.Credential, error) {
	if info.Obfuscated {
		if a.given() {
			fmt.Println(vdCredentialNotUsed)
		}
		return nil, nil
	}
	return vdResolveCredential(a, false)
}

// ---------------------------------------------------------------- verify

// vdVerify checks one container or several from the file, read-only:
// filedo <file.fdd ..|mask> verify [password]. The credential is asked once,
// for the first encrypted container, and used for every other one.
func vdVerify(args []string) error {
	if len(args) < 1 {
		return vdUsagef("verify needs a container: filedo <file.fdd> verify [password]")
	}
	var cred credArg
	words := []string{args[0]}
	for _, w := range args[1:] {
		if a, ok := credentialToken(w); ok {
			if cred.given() {
				return vdUsagef("one credential per command: give p:, pf:, pe: or k: once")
			}
			cred = a
			continue
		}
		words = append(words, w)
	}
	defer vdForgetEnv(cred)
	targets, err := vdTargets(words)
	if err != nil {
		return err
	}
	var key fdsec.Credential
	var keyErr error
	resolved := false
	defer func() { clear(key) }()
	return vdEach(targets, "verify", func(p string) error {
		info, err := vdisk.Inspect(p)
		if err != nil {
			return err
		}
		var c fdsec.Credential
		if info.Obfuscated {
			if cred.given() {
				fmt.Println(vdCredentialNotUsed)
			}
		} else {
			if !resolved {
				key, keyErr = vdResolveCredential(cred, false)
				resolved = true
			}
			if keyErr != nil {
				return keyErr
			}
			c = key
		}
		return vdVerifyOne(p, info, c, cred, len(targets) == 1)
	})
}

func vdVerifyOne(path string, info vdisk.Info, cred fdsec.Credential, a credArg, single bool) error {
	c, err := vdisk.Open(vdContext(), path, cred, vdisk.OpenRead)
	if err != nil {
		return vdCredentialErr(err, a)
	}
	defer c.Close()
	return vdVerifyOpened(path, info, c, single)
}

// vdVerifyOpened is verify on a container already open for reading - a file,
// or a partition disk through its brokered handle (SP-0148 8.4).
func vdVerifyOpened(path string, info vdisk.Info, c *vdisk.Container, single bool) error {
	line := vdPercent("Reading")
	r, verr := c.Verify(vdContext(), line)
	line.end()
	if errors.Is(verr, vdisk.ErrStopped) {
		return verr
	}
	headers := "the primary opens; the backup is " + r.Backup
	if r.FromBackup {
		headers = "the primary does NOT open; the backup carried the container"
	}
	clean := "yes"
	if !r.Clean {
		clean = "no"
	}
	fmt.Printf("Container:     %s\n", path)
	fmt.Printf("Protection:    %s\n", vdProtectionNote(info))
	fmt.Printf("Header pair:   %s\n", headers)
	fmt.Printf("Closed clean:  %s\n", clean)
	if info.SaveInProgress {
		fmt.Printf("Save:          %s\n", vdSaveInterruptedNote(info)) // AUD-36-F1 clause 3
	}
	fmt.Printf("Clusters:      %d allocated, %d read\n", r.AllocatedClusters, r.ClustersRead)
	for _, n := range r.Notes {
		fmt.Printf("Note:          %s\n", n)
	}
	for _, p := range r.Problems {
		fmt.Printf("PROBLEM:       %s\n", p)
	}
	if single {
		runNumber("clusters_allocated", r.AllocatedClusters)
		runNumber("clusters_read", r.ClustersRead)
		runNumber("problems", len(r.Problems))
	}
	if verr != nil {
		fmt.Printf("Result:        DAMAGED - %d problem(s) in the container's structure.\n", len(r.Problems))
		return verr
	}
	fmt.Println("Result:        no damage found; every allocated cluster was read. Format 1.0 has no digest table, so the data was read, not verified.")
	return nil
}

// ---------------------------------------------------------------- export

// vdExportTreeRefusal is the file-tree export this build does not carry, with
// the route that does exist.
const vdExportTreeRefusal = "exporting the files inside the volume is not in this build; export the raw image instead, which any NTFS reader opens, Windows included: filedo <file.fdd> export <dest> raw (or vhd)"

// vdExport writes the volume out of the container with no mount:
// filedo <file.fdd> export <dest> [raw|vhd] [password], or `to <dest>`.
func vdExport(args []string) error {
	o, err := vdParseExport(args)
	defer vdForgetEnv(o.cred)
	if err != nil {
		return err
	}
	src, dest, form, cred := o.src, o.dest, o.form, o.cred
	if form < 0 {
		return fmt.Errorf("%w: %s", vdisk.ErrUnsupported, vdExportTreeRefusal)
	}
	if _, err := os.Stat(dest); err == nil {
		return vdUsagef("%s exists; an export never writes over a file", dest)
	}
	info, err := vdisk.Inspect(src)
	if err != nil {
		return err
	}
	key, err := vdCredentialFor(info, cred)
	if err != nil {
		return err
	}
	defer clear(key)
	c, err := vdisk.Open(vdContext(), src, key, vdisk.OpenRead)
	if err != nil {
		return vdCredentialErr(err, cred)
	}
	defer c.Close()
	return vdExportOpened(src, dest, form, info, c)
}

// vdExportOpened is export from a container already open for reading.
func vdExportOpened(src, dest string, form int, info vdisk.Info, c *vdisk.Container) error {
	line := vdPercent("Exporting")
	err := c.ExportRaw(vdContext(), dest, vdisk.RawForm(form), line)
	line.end()
	if err != nil {
		if errors.Is(err, vdisk.ErrStopped) {
			return fmt.Errorf("%w; the partial image was removed", err)
		}
		return err
	}
	what := "a raw image of the volume's bytes"
	if vdisk.RawForm(form) == vdisk.RawFormVHD {
		what = "a fixed VHD (the volume's bytes and a 512-byte footer), which Windows attaches with Disk Management or Mount-DiskImage"
	}
	fmt.Printf("Exported the %s volume of %s to %s as %s. %s is unchanged.\n", vdSize(info.LogicalSize), src, dest, what, src)
	if !info.Obfuscated {
		fmt.Println("The image is not encrypted: anyone who has it reads the volume without any credential.")
	}
	runNumber("bytes_written", info.LogicalSize)
	vdLogf("export %s to %s (form %d)", src, dest, form)
	return nil
}

// vdExportOpts is a parsed export line; form -1 is the file tree.
type vdExportOpts struct {
	src, dest string
	form      int
	cred      credArg
}

// vdParseExport reads export's words in any order: the destination (the
// first word that is no option, or the word after to), raw or vhd, and the
// credential.
func vdParseExport(args []string) (vdExportOpts, error) {
	o := vdExportOpts{form: -1}
	if len(args) < 1 {
		return o, vdUsagef("export needs a container: filedo <file.fdd> export <dest> raw|vhd")
	}
	o.src = args[0]
	for i := 1; i < len(args); i++ {
		w := args[i]
		if a, ok := credentialToken(w); ok {
			if o.cred.given() {
				return o, vdUsagef("one credential per command: give p:, pf:, pe: or k: once")
			}
			o.cred = a
			continue
		}
		switch lw := strings.ToLower(w); {
		case lw == "raw" || lw == "vhd":
			f := int(vdisk.RawFormImage)
			if lw == "vhd" {
				f = int(vdisk.RawFormVHD)
			}
			if o.form >= 0 && o.form != f {
				return o, vdUsagef("export writes raw or vhd, not both")
			}
			o.form = f
		case lw == "to":
			if i+1 >= len(args) || o.dest != "" {
				return o, vdUsagef("to needs one destination: filedo <file.fdd> export to <dest> raw")
			}
			o.dest = args[i+1]
			i++
		case o.dest == "":
			o.dest = w
		default:
			// Never quoted back: the word may be a password without p:.
			return o, vdUsagef("unknown word %d for export: want <dest>, raw, vhd, or a credential as p:, pf:, pe: or k:", i+1)
		}
	}
	if o.dest == "" {
		return o, vdUsagef("export needs a destination: filedo <file.fdd> export <dest> raw|vhd")
	}
	if err := vdRefuseCredentialSlot("destination slot", o.dest); err != nil {
		return o, err
	}
	return o, nil
}

// ---------------------------------------------------------------- compact

// vdCompact returns unused space of the file to the file system:
// filedo <file.fdd> compact [password].
func vdCompact(args []string, batch bool) error {
	if len(args) < 1 {
		return vdUsagef("compact needs a container: filedo <file.fdd> compact [password]")
	}
	path := args[0]
	cred, err := vdCredOnly("compact", args[1:])
	if err != nil {
		return err
	}
	defer vdForgetEnv(cred)
	info, err := vdisk.Inspect(path)
	if err != nil {
		return err
	}
	if err := vdRefuseMounted(path, info, "compact"); err != nil {
		return err
	}
	if err := vdOfflineWriterGate(path, info, "compact", batch); err != nil {
		return err
	}
	key, err := vdCredentialFor(info, cred)
	if err != nil {
		return err
	}
	defer clear(key)
	vdWriterStamp()
	c, err := vdisk.Open(vdContext(), path, key, vdisk.OpenWrite)
	if err != nil {
		return vdCredentialErr(err, cred)
	}
	before := info.FileSize
	line := vdPercent("Compacting")
	err = c.Compact(vdContext(), line)
	line.end()
	cerr := c.Close()
	if err != nil {
		if errors.Is(err, vdisk.ErrStopped) && cerr == nil {
			return fmt.Errorf("%w; %s is closed cleanly at its last committed state", err, path)
		}
		return err
	}
	if cerr != nil {
		return cerr
	}
	after := before
	if fi, err := os.Stat(path); err == nil {
		after = fi.Size()
	}
	fmt.Printf("Compacted %s: the file was %s and is %s now. The volume inside is unchanged.\n",
		path, formatBytes(uint64(before)), formatBytes(uint64(after)))
	runNumber("bytes_before", before)
	runNumber("bytes_after", after)
	vdLogf("compact %s: %d -> %d bytes", path, before, after)
	return nil
}

// ---------------------------------------------------------------- grow

// vdGrow makes the volume larger: filedo <file.fdd> grow <size> [password].
// The size is parseSize's, as for speed and fill, without speed's 10 GiB
// bound (spec 5.4).
func vdGrow(args []string, batch bool) error {
	if len(args) < 2 {
		return vdUsagef("grow needs the new size: filedo <file.fdd> grow <size>, like 20G")
	}
	path := args[0]
	newSize, err := vdParseSize(args[1], "grow")
	if err != nil {
		return err
	}
	cred, err := vdCredOnly("grow", args[2:])
	if err != nil {
		return err
	}
	defer vdForgetEnv(cred)
	info, err := vdisk.Inspect(path)
	if err != nil {
		return err
	}
	if err := vdRefuseMounted(path, info, "grow"); err != nil {
		return err
	}
	if newSize <= info.LogicalSize {
		return vdUsagef("grow makes a volume larger: %s is not above its %s now", vdSize(newSize), vdSize(info.LogicalSize))
	}
	if err := vdOfflineWriterGate(path, info, "grow", batch); err != nil {
		return err
	}
	key, err := vdCredentialFor(info, cred)
	if err != nil {
		return err
	}
	defer clear(key)
	vdWriterStamp()
	c, err := vdisk.Open(vdContext(), path, key, vdisk.OpenWrite)
	if err != nil {
		return vdCredentialErr(err, cred)
	}
	err = c.Grow(vdContext(), newSize)
	cerr := c.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	fmt.Printf("Grew the volume of %s from %s to %s.\n", path, vdSize(info.LogicalSize), vdSize(newSize))
	fmt.Println("Windows sees the larger disk at the next mount. The volume inside keeps its old size until it is extended there: \"Extend Volume\" in Disk Management, or Resize-Partition in PowerShell.")
	runNumber("volume_bytes", newSize)
	vdLogf("grow %s: %d -> %d bytes", path, info.LogicalSize, newSize)
	return nil
}

// vdParseSize reads a volume size with the repository's parseSize. The word is
// never quoted back: a password typed in the size slot must not reach
// history.json through the message.
func vdParseSize(word, verb string) (int64, error) {
	mb, err := parseSize(word)
	if err != nil {
		return 0, vdUsagef("the size word of %s is a size like 20G, 512M or 1.5T", verb)
	}
	return int64(mb) << 20, nil
}

// ---------------------------------------------------------------- pass

// vdPassEmpty refuses an empty new credential (owner decision 1 of P6): the
// one way a volume loses its credential is an obfuscated copy.
func vdPassEmpty(path string) error {
	return vdUsagef("an encrypted container is never given an empty credential, and the credential was not changed; to remove the password, make an obfuscated copy: filedo %s clone <new.fdd> nopass", path)
}

// vdPass changes the one credential of an encrypted container:
// filedo <file.fdd> pass [<old credential>] [new <new credential>]. Omitted,
// the current one is asked once and the new one twice. The new one is never a
// bare word.
func vdPass(args []string) error {
	if len(args) < 1 {
		return vdUsagef("pass needs a container: filedo <file.fdd> pass [<current credential>] [new <new credential>]")
	}
	path := args[0]
	var old, next credArg
	sawNew := false
	for i := 1; i < len(args); i++ {
		w := args[i]
		if strings.EqualFold(w, "new") && !sawNew {
			sawNew = true
			if i+1 < len(args) {
				a, ok := credentialToken(args[i+1])
				if !ok {
					return vdUsagef("the word after new is the new credential as p:, pf:, pe: or k: - never a bare word")
				}
				next = a
				i++
			}
			continue
		}
		if a, ok := credentialToken(w); ok && !sawNew && !old.given() {
			old = a
			continue
		}
		// Never quoted back: the word may be a password without its prefix.
		return vdUsagef("unknown word %d for pass: want [<current credential>] [new <new credential>], each as p:, pf:, pe: or k:", i+1)
	}
	defer vdForgetEnv(old)
	defer vdForgetEnv(next)
	if next.src == "p" && next.val == "" {
		return vdPassEmpty(path)
	}
	info, err := vdisk.Inspect(path)
	if err != nil {
		return err
	}
	if info.Obfuscated {
		return vdUsagef("%s has no credential to change; it is obfuscated, not encrypted, and opens without one", path)
	}
	if info.Profile == vdisk.ProfileSealed {
		return vdUsagef("%s is sealed and is never written again, its key slot included; clone it under the new credential instead", path)
	}
	if err := vdRefuseMounted(path, info, "pass"); err != nil {
		return err
	}
	oldKey, err := vdResolveCredentialAs(old, false, "Current credential (no echo): ")
	if err != nil {
		return err
	}
	defer clear(oldKey)
	newKey, err := vdResolveCredentialAs(next, true, "New credential (no echo): ")
	if err != nil {
		return err
	}
	defer clear(newKey)
	if len(newKey) == 0 {
		return vdPassEmpty(path)
	}
	if err := vdisk.ChangeCredential(path, oldKey, newKey, next.keyfile()); err != nil {
		return vdCredentialErr(err, old)
	}
	fmt.Printf("Changed the credential of %s. The data was not rewritten: only the key slot changed, and the data key inside it is the same.\n", path)
	fmt.Println("A copy of this file made before now still opens with the old credential.")
	if next.keyfile() {
		fmt.Printf("The new credential is the keyfile %s: losing that file loses the data.\n", next.val)
	}
	vdLogf("pass %s: credential changed (keyfile=%v)", path, next.keyfile())
	return nil
}

package main

import (
	"html"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The cross-surface check of SP-0004 P6 (tactical plan section 7 and the G3 gate).
//
// G3 asks one question of every surface the virtual disks reach: does it tell the truth about
// obfuscation, encryption, and what the platform cannot guarantee? A proofread answers it once; this
// answers it on every run. It reads what a user reads - the five READMEs, the site guide and the
// landing page in their three authored locales, the Store listing sources, the winget description and
// the shell's five locale tables - and holds each to four claims:
//
//   - an obfuscated (empty-credential) container is never called encrypted or protected: every clause
//     that names obfuscation carries the word for encrypted only in a negated or a neutral either/or
//     form, and each surface says "obfuscated, not encrypted" in so many words;
//   - verify reads, it does not prove (format 1.0 has no digest table);
//   - a password is never removed in place;
//   - the platform limits: Windows only, mounting needs administrator rights, and the Microsoft Store
//     build cannot mount.
//
// Like fdsec_surfaces_test.go it does not diff translated prose. It anchors on short, concrete
// phrases the surfaces were written with, per language, so a reworded sentence that keeps the claim
// only needs its anchor moved, and a sentence that loses the claim fails here.

// vdSurfaceLang is one language's vocabulary for the checks above. Every string is matched in lower
// case.
type vdSurfaceLang struct {
	code string
	// Stems that make a clause an obfuscation clause.
	obfuscation []string
	// Stems of the words that must not describe an obfuscated container: encrypted, protected.
	encrypted []string
	// Phrases removed from a clause before it is judged: the negated forms ("not encrypted") and the
	// neutral either/or forms ("obfuscated or encrypted") that name both without equating them.
	allowed []string
	// The sentence every surface in this language must carry, as a pattern: the gendered languages
	// inflect both words.
	notEncrypted string
	// The platform limits and the two truths about verify and the password.
	windowsOnly     string
	administrator   string
	storeCannot     string
	readNotVerified string
	neverInPlace    string
}

var vdSurfaceLangs = map[string]vdSurfaceLang{
	"en": {
		code:        "en",
		obfuscation: []string{"obfuscat"},
		encrypted:   []string{"encrypt", "protect"},
		allowed: []string{
			"not encrypted", "unencrypted", "obfuscated or encrypted", "differs from encryption",
			"differs from an encrypted one", "not an encrypted one", "is not encryption",
		},
		notEncrypted:    "obfuscated, not encrypted",
		windowsOnly:     "windows only",
		administrator:   "administrator",
		storeCannot:     "cannot mount",
		readNotVerified: "read, not verified",
		neverInPlace:    "never removed in place",
	},
	"ru": {
		code:        "ru",
		obfuscation: []string{"замаскир", "маскиров"},
		encrypted:   []string{"зашифр", "шифров", "защищ"},
		allowed: []string{
			"не зашифрован", "незашифрован", "замаскирован или зашифрован",
			"замаскирован он или зашифрован", "отличается от шифрования", "отличается от зашифрованного",
			// "an obfuscated container needs no password, an encrypted one does"
			"пароль не нужен, зашифрованному",
		},
		notEncrypted:    `замаскирован\pL*, а не зашифрован`,
		windowsOnly:     "только windows",
		administrator:   "администратор",
		storeCannot:     "не может подключать",
		readNotVerified: "прочитаны, но не сверены",
		neverInPlace:    "никогда не снимается на месте",
	},
	"ua": {
		code:        "ua",
		obfuscation: []string{"замаскован", "маскуванн"},
		encrypted:   []string{"зашифр", "шифруванн", "захищ"},
		allowed: []string{
			"не зашифрован", "незашифрован", "замаскований чи зашифрований",
			"замаскований він чи зашифрований", "відрізняється від шифрування",
			"відрізняється від зашифрованого",
			"пароль не потрібен, зашифрованому",
		},
		notEncrypted:    `замаскован\pL*, а не зашифрован`,
		windowsOnly:     "лише windows",
		administrator:   "адміністратор",
		storeCannot:     "не може підключати",
		readNotVerified: "прочитано, але не звірено",
		neverInPlace:    "ніколи не знімається на місці",
	},
	"de": {
		code:            "de",
		obfuscation:     []string{"verschleier"},
		encrypted:       []string{"verschlüssel", "schützt", "geschützt"},
		allowed:         []string{"nicht verschlüsselt", "unverschlüsselt", "verschleiert oder verschlüsselt"},
		notEncrypted:    "verschleiert, nicht verschlüsselt",
		windowsOnly:     "nur windows",
		administrator:   "administrator",
		storeCannot:     "kann keine container einbinden",
		readNotVerified: "gelesen, nicht verifiziert",
		neverInPlace:    "nie an ort und stelle entfernt",
	},
	"fr": {
		code:            "fr",
		obfuscation:     []string{"camoufl"},
		encrypted:       []string{"chiffr", "protég"},
		allowed:         []string{"pas chiffré", "non chiffré", "camouflé ou chiffré"},
		notEncrypted:    `camouflée?, pas chiffrée?`,
		windowsOnly:     "windows uniquement",
		administrator:   "administrateur",
		storeCannot:     "ne peut pas monter",
		readNotVerified: "lues, pas vérifiées",
		neverInPlace:    "jamais retiré sur place",
	},
}

// vdClauseSplit cuts prose into the clauses a claim lives in: sentence ends, semicolons, colons and
// the house style's spaced hyphen. Commas are cut by vdClauses, which keeps an appositive together.
var vdClauseSplit = regexp.MustCompile(`[.;:!?](?:\s|$)|\s-\s`)

// vdClauses splits flattened prose into clauses. A comma ends a clause too, unless the word after it
// is a word for encrypted: "obfuscated, encrypted container" is one claim about one container, and it
// is judged as one.
func vdClauses(l vdSurfaceLang, flat string) []string {
	var out []string
	for _, part := range vdClauseSplit.Split(flat, -1) {
		pieces := strings.Split(part, ", ")
		cur := pieces[0]
		for _, next := range pieces[1:] {
			if vdStartsWithAny(next, l.encrypted) {
				cur += ", " + next
				continue
			}
			out = append(out, cur)
			cur = next
		}
		out = append(out, cur)
	}
	return out
}

func vdStartsWithAny(s string, stems []string) bool {
	for _, st := range stems {
		if strings.HasPrefix(s, st) {
			return true
		}
	}
	return false
}

// vdObfuscationClauses returns every clause of text that names obfuscation and still carries a word
// for encrypted or protected once the negated and neutral forms are removed. Each one is a surface
// calling an obfuscated container encrypted.
func vdObfuscationClauses(l vdSurfaceLang, text string) []string {
	var bad []string
	flat := strings.ToLower(strings.Join(strings.Fields(text), " "))
	for _, clause := range vdClauses(l, flat) {
		if !vdContainsAny(clause, l.obfuscation) {
			continue
		}
		judged := clause
		for _, a := range l.allowed {
			judged = strings.ReplaceAll(judged, a, " ")
		}
		if vdContainsAny(judged, l.encrypted) {
			bad = append(bad, strings.TrimSpace(clause))
		}
	}
	return bad
}

func vdContainsAny(s string, stems []string) bool {
	for _, st := range stems {
		if strings.Contains(s, st) {
			return true
		}
	}
	return false
}

// vdReadmeSection is the "## " section of a README whose heading names .fdd - the one surface of a
// README the virtual disks own.
func vdReadmeSection(t *testing.T, body, rel string) string {
	t.Helper()
	body = strings.ReplaceAll(body, "\r\n", "\n")
	for _, sec := range strings.Split(body, "\n## ") {
		head, _, _ := strings.Cut(sec, "\n")
		if strings.Contains(head, "`.fdd`") {
			return sec
		}
	}
	t.Fatalf("%s has no \"## \" section about .fdd virtual disks", rel)
	return ""
}

// vdSpanText gathers the text of a hand-authored page per site language (data-l="ru|en|ua"), tags
// removed and entities decoded, so a claim reads as the visitor reads it.
var (
	vdSpanRe = regexp.MustCompile(`(?s)<span data-l="(ru|en|ua)"\s*>(.*?)</span\s*>`)
	vdTagRe  = regexp.MustCompile(`(?s)<[^>]*>`)
)

func vdSpanText(page string) map[string]string {
	out := map[string]string{}
	for _, m := range vdSpanRe.FindAllStringSubmatch(page, -1) {
		out[m[1]] += " " + html.UnescapeString(vdTagRe.ReplaceAllString(m[2], " ")) + "\n"
	}
	return out
}

// vdCheckProse holds one surface in one language to the claims. full asks for all of them - a
// README section or the guide; otherwise only that obfuscation is never called encryption and that
// the obfuscation sentence is there.
func vdCheckProse(t *testing.T, where string, l vdSurfaceLang, text string, full bool) {
	t.Helper()
	low := strings.ToLower(strings.Join(strings.Fields(text), " "))
	for _, c := range vdObfuscationClauses(l, text) {
		t.Errorf("%s (%s) calls an obfuscated container encrypted or protected: %q", where, l.code, c)
	}
	if !regexp.MustCompile(l.notEncrypted).MatchString(low) {
		t.Errorf("%s (%s) never says %q", where, l.code, l.notEncrypted)
	}
	if !full {
		return
	}
	for _, need := range []struct{ what, phrase string }{
		{"that the feature is Windows only", l.windowsOnly},
		{"that mounting needs administrator rights", l.administrator},
		{"that the Microsoft Store build cannot mount", l.storeCannot},
		{"that verify reads and does not prove", l.readNotVerified},
		{"that a password is never removed in place", l.neverInPlace},
	} {
		if !strings.Contains(low, need.phrase) {
			t.Errorf("%s (%s) does not say %s (%q)", where, l.code, need.what, need.phrase)
		}
	}
	if !regexp.MustCompile(`microsoft[ -]store`).MatchString(low) {
		t.Errorf("%s (%s) never names the Microsoft Store build it limits", where, l.code)
	}
	if !strings.Contains(low, "nopass") {
		t.Errorf("%s (%s) does not name clone .. nopass, the one way a password comes off", where, l.code)
	}
	if !strings.Contains(text, "FDD-FORMAT") {
		t.Errorf("%s (%s) does not cite the FDD-FORMAT contract by id", where, l.code)
	}
}

// vdCatalogReference matches the two ways a surface can point into the shared contracts catalog: a
// drive-letter path to a Contracts folder, and a catalog folder-qualified contract file (a lower-case,
// hyphenated function folder, a slash, an upper-case contract document). It is a pattern, not a literal,
// so this file does not itself name the catalog by path.
var vdCatalogReference = regexp.MustCompile(`[A-Za-z]:[\\/]+Contracts\b|\b[a-z]+(?:-[a-z]+)+/[A-Z][A-Z0-9]+(?:-[A-Z0-9]+)+\.md`)

// Every README locale carries a virtual-disks section that tells the truth.
func TestVD_Surfaces_EveryReadmeLocaleTellsTheTruth(t *testing.T) {
	root := repoRoot(t)
	for rel, code := range map[string]string{
		"README.md": "en", "README.ru.md": "ru", "README.ua.md": "ua", "README.de.md": "de", "README.fr.md": "fr",
	} {
		body := readSurface(t, root, rel)
		sec := vdReadmeSection(t, body, rel)
		vdCheckProse(t, rel, vdSurfaceLangs[code], sec, true)
		// The install section names the new MSI feature in its silent-install example.
		if !strings.Contains(body, "ADDLOCAL=Main,ExplorerIntegration,DiskContainerIntegration,DesktopShortcut") {
			t.Errorf("%s's silent-install example does not carry the DiskContainerIntegration feature", rel)
		}
		// A link into the catalog would point at a drive a reader does not have (AGENTS.md, External
		// contracts: cite by id, never link).
		if vdCatalogReference.MatchString(body) {
			t.Errorf("%s links into the contracts catalog instead of citing the contract by id", rel)
		}
	}
}

// The site guide, in the three authored locales, plus its place in the site.
func TestVD_Surfaces_TheGuideTellsTheTruthInEveryAuthoredLocale(t *testing.T) {
	root := repoRoot(t)
	rel := filepath.Join("docs", "guides", "virtual-disks.html")
	page := readSurface(t, root, rel)
	text := vdSpanText(page)
	for _, code := range []string{"ru", "en", "ua"} {
		if strings.TrimSpace(text[code]) == "" {
			t.Fatalf("%s has no %s text at all", rel, code)
		}
		vdCheckProse(t, rel, vdSurfaceLangs[code], text[code], true)
	}

	// Published, reachable and indexed, like every guide (docs/README.md, DOC-EXTERNAL-QUALITY rule 1).
	if !strings.Contains(readSurface(t, root, filepath.Join("docs", "sitemap.xml")), "guides/virtual-disks.html") {
		t.Error("the virtual-disks guide is published but absent from docs/sitemap.xml")
	}
	for _, from := range []string{
		filepath.Join("docs", "guides", "index.html"),
		filepath.Join("docs", "guides", "topics.html"),
		filepath.Join("docs", "index.html"),
	} {
		if !strings.Contains(readSurface(t, root, from), "virtual-disks.html") {
			t.Errorf("%s does not link the virtual-disks guide", from)
		}
	}
	glossary := readSurface(t, root, filepath.Join("docs", "guides", "glossary.html"))
	for _, id := range []string{"virtual-disk", "obfuscation"} {
		if !strings.Contains(glossary, `id="term-`+id+`"`) {
			t.Errorf("the glossary has no entry for %s", id)
		}
	}
	gloss := vdSpanText(glossary)
	for _, code := range []string{"ru", "en", "ua"} {
		for _, c := range vdObfuscationClauses(vdSurfaceLangs[code], gloss[code]) {
			t.Errorf("the glossary (%s) calls obfuscation encryption: %q", code, c)
		}
	}

	// The site's own rules for every guide page: one Install button, and only known stored values.
	start := strings.Index(page, `<header class="site-header">`)
	end := strings.Index(page, "</header>")
	if start < 0 || end < start {
		t.Fatalf("%s has no site header", rel)
	}
	if block := page[start:end]; strings.Count(block, `class="btn`) != 1 || !strings.Contains(block, `href="../#get"`) {
		t.Errorf("%s header must have exactly one Install button to ../#get", rel)
	}
	if !strings.Contains(page, `t !== "dark" && t !== "light"`) || !strings.Contains(page, `l !== "ru" && l !== "en" && l !== "ua"`) {
		t.Errorf("%s does not whitelist stored theme and language values", rel)
	}
}

// The landing page's card: short, but it states the obfuscation sentence and the three limits.
func TestVD_Surfaces_TheLandingPageCardStatesTheLimits(t *testing.T) {
	root := repoRoot(t)
	text := vdSpanText(readSurface(t, root, filepath.Join("docs", "index.html")))
	for _, code := range []string{"ru", "en", "ua"} {
		l := vdSurfaceLangs[code]
		vdCheckProse(t, "docs/index.html", l, text[code], false)
		low := strings.ToLower(strings.Join(strings.Fields(text[code]), " "))
		for _, phrase := range []string{l.windowsOnly, l.administrator, l.storeCannot} {
			if !strings.Contains(low, phrase) {
				t.Errorf("docs/index.html (%s) does not state the limit %q", code, phrase)
			}
		}
	}
}

// The privacy page and the install-trust page say where the block server listens, and no longer
// claim the absence of any socket: the mount path opens one, on loopback only.
func TestVD_Surfaces_ThePrivacyClaimsMatchTheLoopbackServer(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		filepath.Join("docs", "privacy.html"),
		filepath.Join("docs", "guides", "install-trust.html"),
	} {
		page := strings.Join(strings.Fields(readSurface(t, root, rel)), " ")
		if !strings.Contains(page, "127.0.0.1") {
			t.Errorf("%s does not say the block server listens on 127.0.0.1 only", rel)
		}
		for _, stale := range []string{
			"not a single network call",
			"networking libraries are simply not linked",
			"contains no networking code",
			"нет ни одного сетевого вызова",
			"немає жодного мережевого виклику",
		} {
			if strings.Contains(page, stale) {
				t.Errorf("%s still claims %q, which the loopback block server makes untrue", rel, stale)
			}
		}
	}
	privacy := readSurface(t, root, filepath.Join("docs", "privacy.html"))
	if !strings.Contains(privacy, "vdisk-state.json") || !strings.Contains(privacy, ".fdd") {
		t.Error("the privacy page does not list what the virtual disks keep in the state folder")
	}
}

// The Store listing describes only what the packaged build does: the read path, never a mount.
func TestVD_Surfaces_TheStoreListingPromisesNoMount(t *testing.T) {
	root := repoRoot(t)
	notAvailable := map[string]string{
		"en": "not available in", "ru": "недоступно", "ua": "недоступне", "de": "nicht verfügbar", "fr": "n'est pas disponible",
	}
	for file, code := range map[string]string{"en.txt": "en", "ru.txt": "ru", "uk.txt": "ua", "de.txt": "de", "fr.txt": "fr"} {
		rel := filepath.Join("msix", "listing", file)
		body := readSurface(t, root, rel)
		l := vdSurfaceLangs[code]
		vdCheckProse(t, rel, l, body, false)
		if !strings.Contains(body, ".fdd") {
			t.Errorf("%s does not describe the .fdd containers the Store build carries", rel)
		}
		if !strings.Contains(strings.ToLower(body), notAvailable[code]) {
			t.Errorf("%s does not say mounting is not available in the Store edition (%q)", rel, notAvailable[code])
		}
	}
	winget := readSurface(t, root, filepath.Join("winget", "SerZhyAle.FileDO.locale.en-US.yaml"))
	vdCheckProse(t, "winget locale manifest", vdSurfaceLangs["en"], winget, false)
}

// The shell's Disks strings, in all five locale tables: no row calls an obfuscated container
// encrypted, the rows that describe one say "not encrypted" outright, and verify's note says the
// data is read, not verified.
func TestVD_Surfaces_TheShellNeverCallsObfuscationEncryption(t *testing.T) {
	root := repoRoot(t)
	body := readSurface(t, root, filepath.Join("filedo_win_vb", "Localization.vb"))
	rowRe := regexp.MustCompile(`"((?:vd_|purpose_job_vd_|rail_job_vd_)[a-z0-9_]+)\|([^"]*)"`)
	for fn, code := range map[string]string{"EnLines": "en", "RuLines": "ru", "UkLines": "ua", "DeLines": "de", "FrLines": "fr"} {
		start := strings.Index(body, "Private Function "+fn+"() As String()")
		if start < 0 {
			t.Fatalf("Localization.vb has no %s table", fn)
		}
		end := strings.Index(body[start:], "\n    End Function")
		if end < 0 {
			t.Fatalf("the %s table is not terminated", fn)
		}
		l := vdSurfaceLangs[code]
		rows := map[string]string{}
		for _, m := range rowRe.FindAllStringSubmatch(body[start:start+end], -1) {
			rows[m[1]] = m[2]
			for _, c := range vdObfuscationClauses(l, m[2]) {
				t.Errorf("%s %s calls an obfuscated container encrypted or protected: %q", fn, m[1], c)
			}
		}
		if len(rows) == 0 {
			t.Fatalf("%s has no Disks rows", fn)
		}
		for _, key := range []string{"vd_facts_obfuscated", "vd_cred_empty_new", "vd_nopass_note"} {
			if !regexp.MustCompile(l.notEncrypted).MatchString(strings.ToLower(rows[key])) {
				t.Errorf("%s %s does not say %q: %q", fn, key, l.notEncrypted, rows[key])
			}
		}
		if !strings.Contains(strings.ToLower(rows["vd_note_verify"]), l.readNotVerified) {
			t.Errorf("%s vd_note_verify does not say the data is read, not verified: %q", fn, rows["vd_note_verify"])
		}
		if !strings.Contains(rows["vd_pass_empty_new"], "nopass") {
			t.Errorf("%s vd_pass_empty_new does not point at clone .. nopass: %q", fn, rows["vd_pass_empty_new"])
		}
		if !strings.Contains(rows["vd_packaged"], "Microsoft Store") {
			t.Errorf("%s vd_packaged does not name the Microsoft Store build: %q", fn, rows["vd_packaged"])
		}
	}
}

// The clause judge itself, on the sentences it exists to catch and to let through.
func TestVD_Surfaces_TheClauseJudge(t *testing.T) {
	en := vdSurfaceLangs["en"]
	for _, bad := range []string{
		"An empty password gives an obfuscated, encrypted container.",
		"Obfuscated containers are encrypted with a key kept in the file.",
		"The obfuscation protects your data.",
	} {
		if len(vdObfuscationClauses(en, bad)) == 0 {
			t.Errorf("the judge let through %q", bad)
		}
	}
	for _, good := range []string{
		"Without a password a container is obfuscated, not encrypted: anyone with the file reads it.",
		"An obfuscated container needs no password; an encrypted one does.",
		"Profile, size, obfuscated or encrypted, whether it was closed cleanly.",
		"clone with nopass writes an obfuscated copy - and the encrypted original stays as it was.",
		"`clone <new.fdd> nopass` writes an obfuscated copy instead, and the encrypted original stays.",
	} {
		if c := vdObfuscationClauses(en, good); len(c) != 0 {
			t.Errorf("the judge refused a true sentence %q: %q", good, c)
		}
	}
	ru := vdSurfaceLangs["ru"]
	if len(vdObfuscationClauses(ru, "Без пароля контейнер замаскирован и зашифрован.")) == 0 {
		t.Error("the Russian judge let an obfuscated container be called encrypted")
	}
	for _, good := range []string{
		"Без пароля контейнер замаскирован, а не зашифрован.",
		"Замаскированному контейнеру пароль не нужен, зашифрованному - нужен.",
	} {
		if c := vdObfuscationClauses(ru, good); len(c) != 0 {
			t.Errorf("the Russian judge refused the true sentence %q: %q", good, c)
		}
	}
}

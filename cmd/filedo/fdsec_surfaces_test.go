package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The cross-surface check of stage S6.
//
// The exit criterion of that stage is one sentence: every surface says the
// same true thing about what an empty credential means, what wiping the
// original can and cannot promise, where a revealed copy lives and what a
// power loss leaves behind - in every authored locale. A proofread proves
// that once; this proves it on every run, which is the difference between a
// rule and a habit.
//
// What it does NOT do is compare translated prose. It anchors on the tokens
// that survive translation - the path, the words SSD and .fd-sec, the
// localization keys themselves - because a test that matched English
// sentences would either fail on every translation or pass on none.

// repoRoot walks up from the package directory to the repository root. The
// test binary runs in cmd/filedo, so the root is two levels up; it is
// resolved rather than hard-coded relative, so a failure names a real path.
//
// The marker is AGENTS.md. It used to be FDSEC-FORMAT.md, which stopped being
// a file in this repository when the contract moved to the shared catalog -
// and a marker that no longer exists turns this whole gate into a silent skip,
// which is the one failure mode a cross-surface check may not have.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		if os.Getenv("FILEDO_FDSEC_REQUIRE_REPO_ROOT") == "1" {
			t.Fatalf("FDSEC_SURFACES_REPO_ROOT_MISSING: not running inside the repository tree (%v)", err)
		}
		t.Skipf("not running inside the repository tree (%v)", err)
	}
	return root
}

func readSurface(t *testing.T, root, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("surface %s cannot be read: %v", rel, err)
	}
	// A checkout with core.autocrlf (the release runner's) has CRLF in a surface that is LF in the
	// repository; the assertions below spell their line breaks as LF.
	return strings.ReplaceAll(string(body), "\r\n", "\n")
}

// The reveal directory, spelled the same way everywhere. A surface that
// names a different directory sends the user to look in the wrong place.
const revealDirToken = `%LOCALAPPDATA%\FileDO\reveal`

// The four claims, as the tokens that carry them across languages.
type surfaceClaim struct {
	name   string
	token  string
	tokens []string
}

func (c surfaceClaim) match(body string) bool {
	if c.token != "" {
		return strings.Contains(body, c.token)
	}
	for _, tok := range c.tokens {
		if strings.Contains(body, tok) {
			return true
		}
	}
	return false
}

var surfaceClaims = []surfaceClaim{
	{name: "where a revealed copy lives", token: revealDirToken},
	{name: "what wiping cannot promise on flash storage", token: "SSD"},
	{name: "the container's extension", token: ".fd-sec"},
	{name: "what an empty credential means", tokens: []string{"obfuscation", "маскировк", "маскуванн", "Verschleierung", "camouflage"}},
}

// Every authored README locale carries the feature and all of its claims.
// The README family is five files, and a feature that lands in one of them is
// a feature four readers never hear about.
func TestSurfaces_EveryReadmeLocaleCarriesTheClaims(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{"README.md", "README.ru.md", "README.ua.md", "README.de.md", "README.fr.md"} {
		body := readSurface(t, root, rel)
		for _, c := range surfaceClaims {
			if !c.match(body) {
				t.Errorf("%s does not say %s", rel, c.name)
			}
		}
		// The power-loss clause has no single token, but the sweep it
		// describes is always the next start of this program.
		if !strings.Contains(body, "FileDO") {
			t.Errorf("%s never names the program that sweeps the copy", rel)
		}
	}
}

// The non-container exit vocabulary is a scripting contract, so it must not
// drift between the five README locales while their surrounding prose is
// translated (CLI-EVENT-STREAM rule 11 / SP-0012 T8).
func TestSurfaces_EveryReadmeLocaleCarriesTheExitVocabulary(t *testing.T) {
	root := repoRoot(t)
	const block = "0 passed or done, 1 ran and found a defect, 2 could not be verified"
	for _, rel := range []string{"README.md", "README.ru.md", "README.ua.md", "README.de.md", "README.fr.md"} {
		if !strings.Contains(readSurface(t, root, rel), block) {
			t.Errorf("%s does not carry the CLI exit-code vocabulary", rel)
		}
	}
}

// The site pages that describe the feature, in all three locales the site
// authors. A page whose Russian half is missing is a page that silently falls
// back to nothing, because the site hides the spans of the other languages.
func TestSurfaces_TheGuidePageIsCompleteInEveryAuthoredLocale(t *testing.T) {
	root := repoRoot(t)
	page := readSurface(t, root, filepath.Join("docs", "guides", "fd-sec-containers.html"))

	for _, c := range surfaceClaims {
		if !c.match(page) {
			t.Errorf("the fd-sec guide does not say %s", c.name)
		}
	}
	for _, lang := range []string{`data-l="ru"`, `data-l="en"`, `data-l="ua"`} {
		if !strings.Contains(page, lang) {
			t.Errorf("the fd-sec guide has no %s text at all", lang)
		}
	}

	// A public page and the sitemap change in the same commit - the repo's
	// own rule (docs/README.md), and the one that keeps the sitemap from
	// lying by omission.
	sitemap := readSurface(t, root, filepath.Join("docs", "sitemap.xml"))
	if !strings.Contains(sitemap, "guides/fd-sec-containers.html") {
		t.Error("the fd-sec guide is published but absent from docs/sitemap.xml")
	}

	// The privacy page is where the reveal directory has to be exact: it is
	// what the Store's data form is derived from.
	privacy := readSurface(t, root, filepath.Join("docs", "privacy.html"))
	if !strings.Contains(privacy, `%LOCALAPPDATA%\FileDO\reveal`) {
		t.Error("the privacy page does not say where a revealed copy lives")
	}
}

// The install-trust page, against the contract it exists to satisfy.
//
// INSTALL-TRUST rule 1 is four sections in one order, rule 4 forbids a whole
// class of instruction, rule 6 ties the fourth section to the privacy page and
// the Store answers, and rule 7 says the page is reachable from where the
// download is. All four are readable from the files, which is what makes a
// contract at this level testable at all rather than only reviewable.
func TestSurfaces_TheInstallTrustPageKeepsItsContract(t *testing.T) {
	root := repoRoot(t)
	raw := readSurface(t, root, filepath.Join("docs", "guides", "install-trust.html"))
	// A sentence in a hand-authored page is wrapped wherever the formatter
	// wrapped it, so the prose is compared with its whitespace collapsed. The
	// markup below is matched against the raw file, where it is one token.
	page := strings.Join(strings.Fields(raw), " ")

	// Rule 1: four sections, in this order. The anchors are what the order is
	// written in, so reading them in order is reading the rule.
	order := []string{`id="what"`, `id="why"`, `id="what-to-click"`, `id="never"`}
	at := -1
	for _, anchor := range order {
		i := strings.Index(page, anchor)
		if i < 0 {
			t.Fatalf("the trust page has no section %s", anchor)
		}
		if i < at {
			t.Errorf("section %s comes out of the order rule 1 fixes", anchor)
		}
		at = i
	}

	// Rule 1 / T4: the page covers every unsigned artifact published in the release
	// (setup EXE, MSI, ZIP), and section 2 mentions the signed Microsoft Store build.
	for _, artifact := range []string{"-windows-x64.msi", "-windows-x64.zip"} {
		if !strings.Contains(page, artifact) {
			t.Errorf("the trust page does not name artifact %q in section 1", artifact)
		}
	}
	if !strings.Contains(page, "Microsoft Store") {
		t.Error("the trust page does not explain that the Microsoft Store build is signed")
	}

	// Rule 2: the dialog is quoted, in each authored locale, so the user can
	// match the page to the window they are looking at.
	for _, quote := range []string{
		"Windows protected your PC",
		"Windows защитила ваш компьютер",
		"Windows захистила ваш ПК",
	} {
		if !strings.Contains(page, quote) {
			t.Errorf("the trust page never quotes %q", quote)
		}
	}

	// Rule 3: the real reason, including that the certificate is a cost we
	// chose not to pay.
	if !strings.Contains(page, "certificate costs hundreds of dollars a year") {
		t.Error("the trust page does not state the cost that was not paid (rule 3)")
	}

	// Rule 4: never tell a user to weaken their protection. These are the
	// sentences that must not be on the page in any language.
	for _, forbidden := range []string{
		"turn off SmartScreen",
		"disable your antivirus",
		"disable SmartScreen",
		"отключите антивирус",
		"выключите SmartScreen",
		"вимкніть антивірус",
		"run as administrator",
		"запустите от имени администратора",
		"запустіть від імені адміністратора",
		// INSTALL-TRUST 1.2 item K: the per-file "Unblock" checkbox removes the mark of the web that
		// SmartScreen keys on, and rule 4's list of instructions is closed.
		"unblock",
		"разблокировать",
		"розблокувати",
	} {
		if strings.Contains(strings.ToLower(page), strings.ToLower(forbidden)) {
			t.Errorf("the trust page tells the user to weaken their protection: %q (rule 4)", forbidden)
		}
	}

	// Rule 5: every elevation is itemized and the single action that undoes all
	// of it is named.
	if !strings.Contains(page, "filedo fdsec unregister") || !strings.Contains(page, "Uninstall") {
		t.Error("the trust page does not name the one action that removes everything the elevation did (rule 5)")
	}
	if strings.Contains(page, "appears once, during setup") {
		t.Error("the trust page claims UAC appears once during setup, omitting runtime elevations (rule 5)")
	}
	for _, elevationTerm := range []string{"probe", "Recover", "-all-users"} {
		if !strings.Contains(page, elevationTerm) {
			t.Errorf("the trust page does not itemize elevation term %q (rule 5)", elevationTerm)
		}
	}

	// Rule 6: the fourth section says the same thing as the privacy page and
	// the Store answers. The claims are three and they are load-bearing.
	privacy := strings.Join(strings.Fields(readSurface(t, root, filepath.Join("docs", "privacy.html"))), " ")
	listing := readSurface(t, root, filepath.Join("msix", "store-listing.md"))
	manifest := readSurface(t, root, filepath.Join("msix", "AppxManifest.xml"))
	if !strings.Contains(page, "declares no internet capability") {
		t.Error("the trust page does not repeat the no-network claim in the privacy page's words (rule 6)")
	}
	if !strings.Contains(privacy, "declares no internet capability") {
		t.Error("the privacy page no longer carries the claim the trust page agrees with")
	}
	if !strings.Contains(listing, "no data collection") {
		t.Error("the Store data form no longer declares no data collection")
	}
	if !strings.Contains(page, "runFullTrust") || !strings.Contains(manifest, "runFullTrust") {
		t.Error("the trust page and the manifest disagree about the one declared capability (rule 6)")
	}
	if strings.Count(manifest, "Capability Name=") != 1 {
		t.Error("the manifest declares more than one capability; the trust page says it declares exactly one")
	}
	if strings.Contains(page, "writes only where you point") {
		t.Error("the trust page overclaims where FileDO writes (rule 6)")
	}
	if strings.Contains(privacy, "does not request administrator rights") ||
		strings.Contains(privacy, "не запрашивает права администратора") ||
		strings.Contains(privacy, "не запитує права адміністратора") {
		t.Error("the privacy page denies administrator elevations that the code performs (rule 6)")
	}
	if !strings.Contains(privacy, `FileDO\runs`) || !strings.Contains(privacy, `FileDO\reports`) {
		t.Error("the privacy page does not list FileDO runs and reports directories (rule 6)")
	}
	if strings.Contains(privacy, "two files") || strings.Contains(privacy, "два служебных") || strings.Contains(privacy, "два службові") {
		t.Error("the privacy page still refers to 'two files' written locally (rule 6)")
	}

	// Localization: authored wherever the other guides are.
	for _, lang := range []string{`data-l="ru"`, `data-l="en"`, `data-l="ua"`} {
		if !strings.Contains(page, lang) {
			t.Errorf("the trust page has no %s text at all", lang)
		}
	}

	// Rule 7: reachable from where the download is - the release body, the site,
	// the guide hub and every README locale.
	sitemap := readSurface(t, root, filepath.Join("docs", "sitemap.xml"))
	if !strings.Contains(sitemap, "guides/install-trust.html") {
		t.Error("the trust page is published but absent from docs/sitemap.xml")
	}
	for _, rel := range []string{
		filepath.Join(".github", "workflows", "release.yml"),
		filepath.Join("docs", "index.html"),
		filepath.Join("docs", "guides", "index.html"),
		filepath.Join("docs", "guides", "install-and-explorer.html"),
		"README.md", "README.ru.md", "README.ua.md", "README.de.md", "README.fr.md",
	} {
		if !strings.Contains(readSurface(t, root, rel), "install-trust.html") {
			t.Errorf("%s does not link the trust page, so a warned reader there has nowhere to go (rule 7)", rel)
		}
	}
}

// Every string the shell shows exists in all five authored locales.
//
// This is the trap AGENTS.md names: a key added to English alone falls back
// silently rather than failing, so the German user reads English and nobody
// finds out. The keys below are the ones stage S6 added; the test walks the
// five tables in Localization.vb and insists on each key in each of them.
func TestSurfaces_EveryShellStringExistsInAllFiveLocales(t *testing.T) {
	root := repoRoot(t)
	body := readSurface(t, root, filepath.Join("filedo_win_vb", "Localization.vb"))

	blocks := map[string]string{}
	for _, fn := range []string{"EnLines", "RuLines", "UkLines", "DeLines", "FrLines"} {
		start := strings.Index(body, "Private Function "+fn+"() As String()")
		if start < 0 {
			t.Fatalf("Localization.vb has no %s table", fn)
		}
		end := strings.Index(body[start:], "\n    End Function")
		if end < 0 {
			t.Fatalf("the %s table is not terminated", fn)
		}
		blocks[fn] = body[start : start+end]
	}

	keys := []string{
		"rail_group_protect", "rail_job_secure", "rail_job_unsecure", "rail_job_reveal",
		"purpose_job_secure", "purpose_job_unsecure", "purpose_job_reveal",
		"shell_lbl_password", "shell_cred_show", "shell_cred_confirm",
		"shell_cred_empty", "shell_cred_short", "shell_cred_mismatch", "shell_cred_ok",
		"shell_cred_out_of_sight",
		"shell_fdsec_original", "shell_fdsec_keep", "shell_fdsec_del", "shell_fdsec_wipe",
		"shell_fdsec_rename", "shell_fdsec_del_container", "shell_fdsec_keep_copy",
		"shell_fdsec_rw", "shell_fdsec_dest", "shell_fdsec_reveal_where",
		"shell_fdsec_secure_note", "shell_fdsec_unsecure_note", "shell_fdsec_wipe_confirm",
		"shell_fdsec_reveal_running", "shell_fdsec_removing_copy", "shell_btn_remove_copy",
	}
	for fn, block := range blocks {
		for _, k := range keys {
			if !strings.Contains(block, `"`+k+`|`) {
				t.Errorf("%s has no %s - that locale falls back to English in silence", fn, k)
			}
		}
	}

	// The three claims again, this time inside the strings the shell shows:
	// the empty-credential line, the wipe line and the reveal line are where
	// a user meets them, and they must hold in every locale.
	for fn, block := range blocks {
		if !strings.Contains(block, revealDirToken) {
			t.Errorf("%s does not name %s where a revealed copy lives", fn, revealDirToken)
		}
		if !strings.Contains(block, "SSD") {
			t.Errorf("%s does not carry the honest caveat about wiping on SSD", fn)
		}
	}
}

// The credential never reaches a command line from the window.
//
// A structural check, in the spirit of the repository's "one file is allowed
// to name a colour" rule: the shell builds its argument list in one method,
// and the only credential token it may add is the name of an environment
// variable. The password itself travels in that variable, set on the child
// process alone.
func TestSurfaces_TheShellPassesTheCredentialOutOfBand(t *testing.T) {
	root := repoRoot(t)
	view := readSurface(t, root, filepath.Join("filedo_win_vb", "JobView.vb"))

	if !strings.Contains(view, `args.Add("pe:" & CredentialEnvName)`) {
		t.Error("the shell no longer passes the credential by environment-variable name")
	}
	for _, forbidden := range []string{`args.Add(credBox.Text)`, `args.Add("p:" & credBox.Text)`} {
		if strings.Contains(view, forbidden) {
			t.Errorf("the shell puts the password on the command line: %s", forbidden)
		}
	}
	if !strings.Contains(view, "CredentialEnvName") {
		t.Error("the credential variable's name is gone, so nothing carries the password")
	}
}

// The two registry writers say the same thing about the double-click.
//
// They are one contract with two implementations (AGENTS.md), and the failure
// they guard against is silent: the MSI writes one command, `fdsec register`
// writes another, and a machine ends up with a document type that opens
// something the documentation does not describe.
func TestSurfaces_BothRegistryWritersOpenTheSameProgram(t *testing.T) {
	root := repoRoot(t)
	wxs := readSurface(t, root, filepath.Join("packaging", "wix", "FileDO.wxs"))
	reg := readSurface(t, root, filepath.Join("cmd", "filedo", "fdsec_register.go"))

	if !strings.Contains(wxs, `[INSTALLFOLDER]filedo.exe&quot; &quot;%1&quot; unsecure start --pause --no-history`) {
		t.Error("the MSI's .fd-sec open verb does not unsecure and start through the CLI")
	}
	if !strings.Contains(reg, `func fdsecOpenCommand(exe string) string {`) {
		t.Error("fdsecOpenCommand moved or was renamed - the wxs comparison above needs a matching update")
	}
	if !strings.Contains(reg, `"%1" unsecure start`) {
		t.Error("fdsec register lost the double-click's unsecure-and-start command")
	}
}

// PAGE-CONTENT, PAGE-STYLE, and SITE-FAMILY-MAP are implemented by the
// hand-authored site. The landing and privacy pages keep a literal footer;
// guides receive the same grid from guide.js. Keep those three sources in
// lockstep, because a missing family member otherwise has no build failure.
func TestSurfaces_TheFooterCarriesTheFamilyMap(t *testing.T) {
	root := repoRoot(t)
	sources := []string{
		filepath.Join("docs", "index.html"),
		filepath.Join("docs", "privacy.html"),
		filepath.Join("docs", "de", "index.html"),
		filepath.Join("docs", "de", "privacy.html"),
		filepath.Join("docs", "fr", "index.html"),
		filepath.Join("docs", "fr", "privacy.html"),
		filepath.Join("docs", "guides", "guide.js"),
	}
	// SITE-FAMILY-MAP section 2 as the footer must show it: the Tool cell is the label and the Type cell
	// sits under it. 1.4 made the application's own English identity STREAMS Player, not the repository
	// name StreamsPlayer.
	cards := []struct{ name, kind string }{
		{"Fast Media Sorter &amp; Organizer", "Android media sorter"},
		{"Fast Media Sorter for Windows", "Windows media sorter"},
		{"CyrFlip", "Windows layout fixer"},
		{"doc-html-translate", "Windows ebook converter"},
		{"STREAMS Player", "Windows stream player"},
		{"OneClickRunner", "Windows tray launcher"},
		{"Universal Agent Kit", "AI-dev methodology"},
		{"SZA", "Portfolio"},
	}
	urls := []string{
		"https://serzhyale.github.io/FastMediaSorter_mob_v2/",
		"https://serzhyale.github.io/FastMediaSorter_Lite/",
		"https://serzhyale.github.io/CyrFlip/",
		"https://serzhyale.github.io/doc-html-translate/",
		"https://serzhyale.github.io/StreamsPlayer/",
		"https://serzhyale.github.io/OneClickRunner/",
		"https://serzhyale.github.io/universal-agent-kit/",
		"https://sza.od.ua",
	}
	for _, rel := range sources {
		body := readSurface(t, root, rel)
		for _, u := range urls {
			if !strings.Contains(body, u) {
				t.Errorf("%s footer is missing %s", rel, u)
			}
		}
		for _, c := range cards {
			if want := "<b>" + c.name + "</b><span>" + c.kind + "</span>"; !strings.Contains(body, want) {
				t.Errorf("%s footer grid lacks the map's label and type %s", rel, want)
			}
		}
		if strings.Contains(body, "<b>StreamsPlayer</b>") {
			t.Errorf("%s still labels STREAMS Player with its repository name", rel)
		}
		if strings.Contains(body, "github.com/SerZhyAle/OneClickRunner") {
			t.Errorf("%s still links OneClickRunner's repository", rel)
		}
		if strings.Contains(body, "serzhyale@gmail.com") {
			t.Errorf("%s states a contact other than sza@ukr.net", rel)
		}
	}

	for _, header := range []struct {
		rel  string
		href string
	}{
		{filepath.Join("docs", "index.html"), `href="#get"`},
		{filepath.Join("docs", "privacy.html"), `href="./#get"`},
		{filepath.Join("docs", "guides", "index.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "check-storage.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "files-and-copies.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "erase.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "gui-command-builder.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "install-and-explorer.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "install-trust.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "fd-sec-containers.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "glossary.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "topics.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "support.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "release-notes.html"), `href="../#get"`},
	} {
		body := readSurface(t, root, header.rel)
		start := strings.Index(body, `<header class="site-header">`)
		if start < 0 {
			t.Errorf("%s has no site header", header.rel)
			continue
		}
		end := strings.Index(body[start:], "</header>")
		if end < 0 {
			t.Errorf("%s has no complete site header", header.rel)
			continue
		}
		block := body[start : start+end]
		if strings.Count(block, `class="btn`) != 1 || !strings.Contains(block, header.href) {
			t.Errorf("%s header must have exactly one Install button to %s", header.rel, header.href)
		}
	}
}

// SITE-STRUCTURE 0.3 (the Support page "is linked from every page footer") and PAGE-CONTENT
// 1.5 item 2 (a child page carries the contact line and a hub link): the static footer of
// every site page links the support page and the hub, and the Guides label is a localised
// span group, never the bare English word (DOC-EXTERNAL-QUALITY 0.11 section 9 F).
func TestSurfaces_EveryFooterLinksSupportAndTheHub(t *testing.T) {
	root := repoRoot(t)
	bareGuides := regexp.MustCompile(`<a\b[^>]*>\s*Guides\s*</a>`)
	supportLink := regexp.MustCompile(`<a\s+href="(?:[^"]*/)?support\.html"`)
	for _, rel := range sitePages(t, root) {
		body := readSurface(t, root, filepath.Join("docs", filepath.FromSlash(rel)))
		start := strings.Index(body, `<footer class="site-footer">`)
		end := strings.Index(body, `</footer>`)
		if start < 0 || end < start {
			t.Errorf("%s has no site footer", rel)
			continue
		}
		footer := body[start:end]
		if !supportLink.MatchString(footer) {
			t.Errorf("%s: the footer does not link the support page", rel)
		}
		if !strings.Contains(footer, `href="https://sza.od.ua"`) {
			t.Errorf("%s: the footer does not link the hub", rel)
		}
		if !strings.Contains(footer, `href="mailto:sza@ukr.net"`) {
			t.Errorf("%s: the footer does not carry the contact line", rel)
		}
		if bareGuides.MatchString(footer) {
			t.Errorf("%s: the footer carries a bare English Guides label outside a data-l span", rel)
		}
	}
}

func TestSurfaces_TheSiteOnlyTrustsKnownStoredPreferences(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		filepath.Join("docs", "index.html"),
		filepath.Join("docs", "privacy.html"),
		filepath.Join("docs", "guides", "guide.js"),
	} {
		body := readSurface(t, root, rel)
		if !strings.Contains(body, `l !== "ru" && l !== "en" && l !== "ua"`) {
			t.Errorf("%s accepts an unknown sza-lang value", rel)
		}
	}
	for _, rel := range []string{
		filepath.Join("docs", "index.html"),
		filepath.Join("docs", "privacy.html"),
		filepath.Join("docs", "guides", "index.html"),
		filepath.Join("docs", "guides", "check-storage.html"),
		filepath.Join("docs", "guides", "files-and-copies.html"),
		filepath.Join("docs", "guides", "erase.html"),
		filepath.Join("docs", "guides", "gui-command-builder.html"),
		filepath.Join("docs", "guides", "install-and-explorer.html"),
		filepath.Join("docs", "guides", "install-trust.html"),
		filepath.Join("docs", "guides", "fd-sec-containers.html"),
		filepath.Join("docs", "guides", "glossary.html"),
		filepath.Join("docs", "guides", "topics.html"),
		filepath.Join("docs", "guides", "support.html"),
		filepath.Join("docs", "guides", "release-notes.html"),
	} {
		body := readSurface(t, root, rel)
		if !strings.Contains(body, `t !== "dark" && t !== "light"`) ||
			!strings.Contains(body, `l !== "ru" && l !== "en" && l !== "ua"`) {
			t.Errorf("%s does not whitelist stored theme and language values", rel)
		}
	}
}

// PAGE-STYLE sections 6 and 7 (1.2): a page whose languages are all in the DOM ships
// data-lang="en" statically, so a visitor without script sees English instead of the ru,
// en and ua text of every group at once, and its pre-paint reader maps a stored "uk" to
// "ua". The pages are the runtime pages: the landing, the privacy page, the 404 page and
// every guide; the standalone de and fr pages are written in one language and read no
// stored language.
func TestSurfaces_TheRuntimePagesDefaultToEnglishAndMapUkToUa(t *testing.T) {
	root := repoRoot(t)
	pages := []string{
		filepath.Join("docs", "index.html"),
		filepath.Join("docs", "privacy.html"),
		filepath.Join("docs", "404.html"),
	}
	guides, err := filepath.Glob(filepath.Join(root, "docs", "guides", "*.html"))
	if err != nil || len(guides) < 14 {
		t.Fatalf("the guide pages were not found (%d, %v)", len(guides), err)
	}
	for _, g := range guides {
		pages = append(pages, filepath.Join("docs", "guides", filepath.Base(g)))
	}
	for _, rel := range pages {
		body := readSurface(t, root, rel)
		if !strings.Contains(body, `<html lang="en" data-lang="en">`) {
			t.Errorf("%s does not ship data-lang=\"en\" statically on <html>", rel)
		}
		if !strings.Contains(body, `if (l === "uk") l = "ua";`) {
			t.Errorf("%s does not map a stored uk to ua before the whitelist", rel)
		}
		if i, j := strings.Index(body, `if (l === "uk") l = "ua";`), strings.Index(body, `l !== "ru" && l !== "en" && l !== "ua"`); i < 0 || j < 0 || i > j {
			t.Errorf("%s maps uk to ua after the whitelist already dropped it", rel)
		}
	}
}

// PAGE-STYLE section 0 step 1 and the domain README section 2: the vendored web kit is
// the catalog's reference file byte for byte, and no line-ending conversion may change
// what the site serves. The catalog is not reachable from a clone, so the expected
// SHA-256 is recorded here; a re-vendor changes it in the same edit, as it changes the
// pointer docs/contracts/PAGE-STYLE.md. The in-app help set carries a second copy
// (HelpPages.CopyKit), which packaging/check-help.ps1 compares with the first.
func TestSurfaces_TheVendoredKitIsHeldByteForByte(t *testing.T) {
	const catalogKitSHA256 = "e438fef279fea8165ba6db46289c3ab35ac2fbcfaa5b2ea50bb009716eddfac9" // reference/sza-kit.css, 2026-10-17
	root := repoRoot(t)
	for _, rel := range []string{filepath.Join("docs", "kit", "sza-kit.css"), filepath.Join("docs", "help", "sza-kit.css")} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s cannot be read: %v", rel, err)
		}
		sum := sha256.Sum256(raw)
		if got := hex.EncodeToString(sum[:]); got != catalogKitSHA256 {
			t.Errorf("%s is not the catalog kit: SHA-256 %s, want %s (a CRLF checkout or a hand edit changes it)", rel, got, catalogKitSHA256)
		}
	}
	attrs, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(attrs), "\r\n", "\n")
	for _, line := range []string{"docs/kit/sza-kit.css -text", "docs/help/sza-kit.css -text"} {
		if !strings.Contains("\n"+text, "\n"+line+"\n") {
			t.Errorf(".gitattributes does not list %q", line)
		}
	}
}

// sitePages lists every site page under docs as a slash path relative to docs, leaving out
// what is not a site page: the one-line redirects (ru, ua), the kit, the assets and the
// generated in-app help (CONTEXT-HELP).
func sitePages(t *testing.T, root string) []string {
	t.Helper()
	docs := filepath.Join(root, "docs")
	var pages []string
	err := filepath.WalkDir(docs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(docs, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch rel {
			case "ru", "ua", "kit", "assets", "help", "contracts":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(rel, ".html") {
			pages = append(pages, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) < 40 {
		t.Fatalf("only %d site pages were found under docs - the walk is not seeing the site", len(pages))
	}
	return pages
}

// PAGE-STYLE 4.3 (1.4), 9 and the 2026-10-17 amendment item 6: the theme control is
// labelled - the ICON-SET glyph app.theme beside a word in the page language, never the
// text glyph U+25D0 - and its accessible name is the page language too. The drawing is
// the catalog's iconography/glyphs/app.theme.svg inlined (the site has no glyph folder),
// so its path data is pinned here; guide.js names the control in every language.
func TestSurfaces_TheThemeControlIsLabelledWithItsGlyph(t *testing.T) {
	const appThemePath = `M12 22c5.52 0 10-4.48 10-10S17.52 2 12 2 2 6.48 2 12s4.48 10 10 10zm1-17.93c3.94.49 7 3.85 7 7.93s-3.05 7.44-7 7.93V4.07z`
	root := repoRoot(t)
	for _, rel := range sitePages(t, root) {
		body := readSurface(t, root, filepath.Join("docs", filepath.FromSlash(rel)))
		start := strings.Index(body, `<button class="theme-btn" id="themeBtn"`)
		if start < 0 {
			t.Errorf("%s has no theme control", rel)
			continue
		}
		end := strings.Index(body[start:], `</button`)
		if end < 0 {
			t.Errorf("%s: the theme control is not closed", rel)
			continue
		}
		button := body[start : start+end]
		if strings.Contains(body, "◐") {
			t.Errorf("%s still draws the theme control with the text glyph U+25D0", rel)
		}
		if !strings.Contains(button, `<svg`) || !strings.Contains(button, appThemePath) {
			t.Errorf("%s: the theme control does not carry the app.theme glyph", rel)
		}
		langs := []string{"ru", "en", "ua"}
		switch {
		case strings.HasPrefix(rel, "de/"):
			langs = []string{"de"}
		case strings.HasPrefix(rel, "fr/"):
			langs = []string{"fr"}
		}
		for _, l := range langs {
			if !strings.Contains(button, `<span data-l="`+l+`">`) {
				t.Errorf("%s: the theme control has no visible %s name", rel, l)
			}
		}
	}
	js := readSurface(t, root, filepath.Join("docs", "guides", "guide.js"))
	for _, want := range []string{`ru: "Сменить тему"`, `en: "Switch theme"`, `ua: "Змінити тему"`, `de: "Design wechseln"`, `fr: "Changer de thème"`} {
		if !strings.Contains(js, want) {
			t.Errorf("docs/guides/guide.js does not name the theme control: %s", want)
		}
	}
	if strings.Contains(js, "Switch to dark theme") || strings.Contains(js, "Switch to light theme") {
		t.Error("docs/guides/guide.js still renames the theme control in English after a click")
	}
}

// SP-0170 slice S: the Microsoft Store build has its own page, and the places that talk
// about that build point to it - the landing's install block and the Store text of the
// install, support and install-trust guides, in every locale.
func TestSurfaces_TheStoreTextLinksTheStoreBuildPage(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		filepath.Join("docs", "index.html"),
		filepath.Join("docs", "de", "index.html"),
		filepath.Join("docs", "fr", "index.html"),
	} {
		if !strings.Contains(readSurface(t, root, rel), `href="guides/microsoft-store.html"`) {
			t.Errorf("%s does not link guides/microsoft-store.html from the install block", rel)
		}
	}
	for _, guide := range []string{"install-and-explorer.html", "support.html", "install-trust.html"} {
		for _, dir := range []string{"guides", filepath.Join("de", "guides"), filepath.Join("fr", "guides")} {
			rel := filepath.Join("docs", dir, guide)
			if !strings.Contains(readSurface(t, root, rel), `href="microsoft-store.html"`) {
				t.Errorf("%s does not link the Microsoft Store build page", rel)
			}
		}
	}
}

// DOC-EXTERNAL-QUALITY rule 1 (SP-0062 T1): the guide corpus has an entry
// portal, a glossary and a subject index, in every authored locale, and all of
// them are published. packaging/check-external-docs.ps1 holds the finer
// structure (every guide indexed, one glossary entry per termbase term); this
// pins that the pages exist and are reachable, so the portal cannot lose them.
func TestSurfaces_TheGuideHubLeadsToTheGlossaryAndSubjectIndex(t *testing.T) {
	root := repoRoot(t)
	hub := readSurface(t, root, filepath.Join("docs", "guides", "index.html"))
	sitemap := readSurface(t, root, filepath.Join("docs", "sitemap.xml"))
	for _, page := range []string{"glossary.html", "topics.html"} {
		if !strings.Contains(hub, `href="`+page+`"`) {
			t.Errorf("the guide hub does not link %s", page)
		}
		if !strings.Contains(sitemap, "guides/"+page) {
			t.Errorf("%s is published but absent from docs/sitemap.xml", page)
		}
		body := readSurface(t, root, filepath.Join("docs", "guides", page))
		for _, lang := range []string{`data-l="ru"`, `data-l="en"`, `data-l="ua"`} {
			if !strings.Contains(body, lang) {
				t.Errorf("%s has no %s text at all", page, lang)
			}
		}
	}
	glossary := readSurface(t, root, filepath.Join("docs", "guides", "glossary.html"))
	for _, id := range []string{"container", "secure", "unsecure", "reveal", "wipe", "fill", "capacity-test"} {
		if !strings.Contains(glossary, `id="term-`+id+`"`) {
			t.Errorf("the glossary has no entry for %s", id)
		}
	}
}

// SP-0171 W15: the erase pillar has its guide (SITE-REPRESENTATION rule 1). The guide exists in
// every locale; the hub, the subject index, the landing page, the sitemap and the registry list
// it; the positioning source no longer carries the hub exception; and the page keeps what a wipe
// guide may not lose - the typed WIPE question, what --yes skips, the refusals, the honest limits
// - and none of the words the claim deny-list bans. The behaviour is pinned by wipe_safety_test.go;
// this holds that the page says it.
func TestSurfaces_TheEraseGuideIsListedAndKeepsItsCaveats(t *testing.T) {
	root := repoRoot(t)
	guides := map[string][]string{
		filepath.Join("docs", "guides", "erase.html"):       {`data-l="ru"`, `data-l="en"`, `data-l="ua"`},
		filepath.Join("docs", "de", "guides", "erase.html"): {`data-l="de"`},
		filepath.Join("docs", "fr", "guides", "erase.html"): {`data-l="fr"`},
	}
	for rel, langs := range guides {
		body := readSurface(t, root, rel)
		for _, l := range langs {
			if !strings.Contains(body, l) {
				t.Errorf("%s has no %s text", rel, l)
			}
		}
		for _, want := range []string{"WIPE", "--yes", "FILEDO_AUTO_CONFIRM", "SSD", `id="confirm"`, `id="refusals"`, `id="limits"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s lost %q", rel, want)
			}
		}
	}
	runtime := strings.ToLower(readSurface(t, root, filepath.Join("docs", "guides", "erase.html")))
	for _, banned := range []string{"secure erase", "securely", "unrecoverable", "military", "irreversible", "for good", "permanently", "impossible to recover"} {
		if strings.Contains(runtime, banned) {
			t.Errorf("docs/guides/erase.html over-claims with %q (positioning-source.json claimDenyList)", banned)
		}
	}
	for rel, href := range map[string]string{
		filepath.Join("docs", "guides", "index.html"):        `href="erase.html"`,
		filepath.Join("docs", "de", "guides", "index.html"):  `href="erase.html"`,
		filepath.Join("docs", "fr", "guides", "index.html"):  `href="erase.html"`,
		filepath.Join("docs", "guides", "topics.html"):       `href="erase.html#limits"`,
		filepath.Join("docs", "de", "guides", "topics.html"): `href="erase.html#limits"`,
		filepath.Join("docs", "fr", "guides", "topics.html"): `href="erase.html#limits"`,
		filepath.Join("docs", "index.html"):                  `href="guides/erase.html"`,
		filepath.Join("docs", "de", "index.html"):            `href="guides/erase.html"`,
		filepath.Join("docs", "fr", "index.html"):            `href="guides/erase.html"`,
	} {
		if !strings.Contains(readSurface(t, root, rel), href) {
			t.Errorf("%s does not link the erase guide (%s)", rel, href)
		}
	}
	sitemap := readSurface(t, root, filepath.Join("docs", "sitemap.xml"))
	registry := readSurface(t, root, filepath.Join("docs", "DOCUMENT_REGISTRY.jsonl"))
	for _, page := range []string{"guides/erase.html", "de/guides/erase.html", "fr/guides/erase.html"} {
		if !strings.Contains(sitemap, "/FileDO/"+page) {
			t.Errorf("%s is published but absent from docs/sitemap.xml", page)
		}
		if !strings.Contains(registry, `"path":"docs/`+page+`"`) {
			t.Errorf("%s is not in docs/DOCUMENT_REGISTRY.jsonl", page)
		}
	}
	positioning := readSurface(t, root, filepath.Join("packaging", "positioning-source.json"))
	if strings.Contains(positioning, `"allowMissing"`) {
		t.Error("packaging/positioning-source.json still excuses a surface from naming a pillar; the erase guide closed the last such exception")
	}
}

// PAGE-STYLE section 1, "Use the available width": the site is not a centred
// column. The kit's --wide cap and the page-level pixel caps are switched off
// in the layers this repo owns (the kit itself is vendored byte-identical, so
// the override lives after it), and the hero text starts at the left edge.
func TestSurfaces_ThePagesUseTheFullWidth(t *testing.T) {
	root := repoRoot(t)
	css := readSurface(t, root, filepath.Join("docs", "guides", "guide.css"))
	for _, banned := range []string{"min(1760px", "min(1100px", "max-width: 600px;\n  margin: 0 auto", "text-align: center;\n}\n.hero .lead"} {
		if strings.Contains(css, banned) {
			t.Errorf("docs/guides/guide.css caps the page width again: %q", banned)
		}
	}
	if !strings.Contains(css, ".container {\n  max-width: none;") {
		t.Error("docs/guides/guide.css does not release the kit's .container cap")
	}
	for _, rel := range []string{
		"index.html", "privacy.html",
		filepath.Join("de", "index.html"), filepath.Join("de", "privacy.html"),
		filepath.Join("fr", "index.html"), filepath.Join("fr", "privacy.html"),
	} {
		body := readSurface(t, root, filepath.Join("docs", rel))
		if !strings.Contains(body, "use the available width") || !strings.Contains(body, ".container,.site-header") {
			t.Errorf("docs/%s lacks the full-width override after its style rules", rel)
		}
	}
}

// SITE-EXPERIENCE rule 17 (SP-0156): the chrome controls are 44 px under any
// pointer and the light theme carries the tokens that reach 4.5 : 1. The layer
// lives in guides/guide.css and the trailing block of each standalone page's
// inline style (the kit stays byte-identical); the measured numbers are in the
// ticket, this only keeps the layer from being dropped.
func TestSurfaces_TheChromeKeeps44pxTargetsAndLightContrast(t *testing.T) {
	root := repoRoot(t)
	want := []string{
		".seg button{min-width:44px;min-height:44px}",
		// The labelled theme control (PAGE-STYLE 4.3) is a pill that grows with its word, so its floor is a
		// minimum, not a fixed square.
		".theme-btn{flex:none;min-width:44px;min-height:44px}",
		".btn,.copy,.tools-grid a{min-height:44px}",
		".footer-bottom a{display:inline-flex;align-items:center;min-width:44px;min-height:44px}",
		`html[data-theme="light"]{--acc:#237430;--acc-strong:#1f6b27;--gold:#8a5f10;--danger:#b42318}`,
		// Kit e438fef2 draws .seg button in the code ink (1.2 : 1 on the light theme); the layer keeps the muted ink.
		`html[data-theme="light"] .seg button:not([aria-pressed="true"]):not(.on){color:var(--muted)}`,
	}
	for _, rel := range []string{
		filepath.Join("guides", "guide.css"),
		"index.html", "privacy.html",
		filepath.Join("de", "index.html"), filepath.Join("de", "privacy.html"),
		filepath.Join("fr", "index.html"), filepath.Join("fr", "privacy.html"),
	} {
		body := readSurface(t, root, filepath.Join("docs", rel))
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("docs/%s lacks the target-size or contrast rule %s", rel, w)
			}
		}
	}
}

// The front page (owner brief 2026-10-06, SP-0161) names the product in full
// and shows its mark once in the header, offers every language in the header
// group, links the guides and the documentation, keeps the other SZA tools
// visible in the footer and names and links the author. The footer tools grid
// had been hidden by a compacting rule, so the test reads the style block too.
func TestSurfaces_TheFrontPageCarriesItsIdentity(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{"index.html", filepath.Join("de", "index.html"), filepath.Join("fr", "index.html")} {
		body := readSurface(t, root, filepath.Join("docs", rel))
		header := body[strings.Index(body, `<header class="site-header">`):]
		header = header[:strings.Index(header, `</header>`)]
		for _, want := range []string{
			`assets/filedo.svg" width="46" height="46" alt="FileDO"`,
			`<small>Advanced File &amp; Storage Operations Tool</small>`,
			`<nav class="header-nav" aria-labelledby="mainNavTitle">`,
			`<a href="guides/">`,
			`https://github.com/SerZhyAle/FileDO#readme`,
			`<nav class="lang-row"`,
		} {
			if !strings.Contains(header, want) {
				t.Errorf("docs/%s: the header lacks %s", rel, want)
			}
		}
		if strings.Count(body, `assets/filedo.svg"`) != 1 {
			t.Errorf("docs/%s: the product mark must appear once on the page", rel)
		}
		// PAGE-CONTENT 1.3 item 1: the name is at least 24 px at 360 px (1.5rem) and 28 px at 1280 px.
		if !strings.Contains(body, ".brand-name b{font-size:clamp(1.5rem,2.3vw,1.8rem)") {
			t.Errorf("docs/%s: the header name is smaller than 24 px at 360 px", rel)
		}
		if regexp.MustCompile(`(?s)\.tools-grid\s*\{\s*display:\s*none`).MatchString(body) {
			t.Errorf("docs/%s hides the other-tools footer grid", rel)
		}
		for _, want := range []string{`<nav class="tools-grid"`, `<a href="https://sza.od.ua">Serhii Zhyhunenko</a>`, `https://github.com/SerZhyAle`} {
			if !strings.Contains(body, want) {
				t.Errorf("docs/%s: the footer lacks %s", rel, want)
			}
		}
	}
}

// SITE-EXPERIENCE rules 15 and 16 (SP-0155): every published page opens with a
// skip link that lands on its one main, and every nav landmark carries a
// name. The footer tools nav of the guide pages is injected by guide.js, so
// that one is read from the script, and a nav that points at an id the page
// never defines would be an unnamed landmark for a screen reader.
func TestSurfaces_EveryPageHasASkipLinkAndLabelledLandmarks(t *testing.T) {
	root := repoRoot(t)
	docs := filepath.Join(root, "docs")
	guideJS := readSurface(t, root, filepath.Join("docs", "guides", "guide.js"))
	guideCSS := readSurface(t, root, filepath.Join("docs", "guides", "guide.css"))
	for _, rule := range []string{".skip-link", ".sr-only"} {
		if !strings.Contains(guideCSS, rule) {
			t.Errorf("docs/guides/guide.css has no %s rule", rule)
		}
	}
	for _, token := range []string{
		`<p class="footer-tools-title" id="footerToolsTitle">`,
		`<nav class="tools-grid" aria-labelledby="footerToolsTitle">`,
	} {
		if !strings.Contains(guideJS, token) {
			t.Errorf("docs/guides/guide.js no longer injects %s", token)
		}
	}

	skipRe := regexp.MustCompile(`(?s)<body>\s*<a class="skip-link" href="#([a-z]+)">(.*?)</a>`)
	mainRe := regexp.MustCompile(`<main[^>]*\sid="([a-z]+)"`)
	navRe := regexp.MustCompile(`<nav\b[^>]*>`)
	labelRe := regexp.MustCompile(`aria-label(?:ledby)?="([^"]+)"`)
	idRe := regexp.MustCompile(`aria-labelledby="([^"]+)"`)

	pages := 0
	err := filepath.WalkDir(docs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(docs, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			// ru/ and ua/ hold one-line redirects to the shared pages.
			switch rel {
			case "ru", "ua", "kit", "assets", "help": // help/ is the generated in-app help, not site pages (CONTEXT-HELP)
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".html") {
			return nil
		}
		pages++
		body := readSurface(t, root, filepath.Join("docs", filepath.FromSlash(rel)))

		skip := skipRe.FindStringSubmatch(body)
		if skip == nil {
			t.Errorf("%s: the skip link is not the first element of body", rel)
		} else {
			if n := strings.Count(body, `<main`); n != 1 {
				t.Errorf("%s: expected one main, found %d", rel, n)
			}
			if m := mainRe.FindStringSubmatch(body); m == nil || m[1] != skip[1] {
				t.Errorf("%s: the skip link targets #%s but main does not carry that id", rel, skip[1])
			}
			lang := "ru"
			switch {
			case strings.HasPrefix(rel, "de/"):
				lang = "de"
			case strings.HasPrefix(rel, "fr/"):
				lang = "fr"
			}
			if !strings.Contains(skip[2], `data-l="`+lang+`"`) {
				t.Errorf("%s: the skip link has no %s text", rel, lang)
			}
			if lang == "ru" && (!strings.Contains(skip[2], `data-l="en"`) || !strings.Contains(skip[2], `data-l="ua"`)) {
				t.Errorf("%s: the skip link is missing its en or ua text", rel)
			}
		}

		if !strings.Contains(body, `<nav aria-labelledby="footerLinksTitle">`) || !strings.Contains(body, `id="footerLinksTitle"`) {
			t.Errorf("%s: the footer links are not a labelled nav", rel)
		}
		if strings.HasSuffix(rel, "index.html") && !strings.Contains(rel, "guides/") || strings.HasSuffix(rel, "privacy.html") {
			if !strings.Contains(body, `<nav class="tools-grid" aria-labelledby="footerToolsTitle">`) || !strings.Contains(body, `id="footerToolsTitle"`) {
				t.Errorf("%s: the footer tools grid is not a labelled nav", rel)
			}
			if !strings.Contains(body, ".skip-link{") || !strings.Contains(body, ".sr-only{") {
				t.Errorf("%s: its own style block lacks the skip-link or sr-only rule", rel)
			}
		}

		for _, tag := range navRe.FindAllString(body, -1) {
			m := labelRe.FindStringSubmatch(tag)
			if m == nil {
				t.Errorf("%s: unnamed nav landmark %s", rel, tag)
				continue
			}
			if id := idRe.FindStringSubmatch(tag); id != nil {
				if !strings.Contains(body, `id="`+id[1]+`"`) && !strings.Contains(guideJS, `id="`+id[1]+`"`) {
					t.Errorf("%s: %s names an id nothing defines", rel, tag)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if pages < 40 {
		t.Errorf("only %d site pages were found under docs - the walk is not seeing the site", pages)
	}
}

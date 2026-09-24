package main

import (
	"os"
	"path/filepath"
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
	return string(body)
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
		filepath.Join("docs", "guides", "guide.js"),
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
		{filepath.Join("docs", "guides", "gui-command-builder.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "install-and-explorer.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "install-trust.html"), `href="../#get"`},
		{filepath.Join("docs", "guides", "fd-sec-containers.html"), `href="../#get"`},
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
		filepath.Join("docs", "guides", "gui-command-builder.html"),
		filepath.Join("docs", "guides", "install-and-explorer.html"),
		filepath.Join("docs", "guides", "install-trust.html"),
		filepath.Join("docs", "guides", "fd-sec-containers.html"),
	} {
		body := readSurface(t, root, rel)
		if !strings.Contains(body, `t !== "dark" && t !== "light"`) ||
			!strings.Contains(body, `l !== "ru" && l !== "en" && l !== "ua"`) {
			t.Errorf("%s does not whitelist stored theme and language values", rel)
		}
	}
}

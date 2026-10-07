# INSTALL-TRUST

| | |
| --- | --- |
| **Id** | `INSTALL-TRUST` |
| **Version** | 1.2 (no wire carrier - a shipped user-facing document) |
| **Role** | producer - the setup EXE and the MSI are unsigned, so users meet SmartScreen |
| **Home** | shared contracts catalog, folder `install-trust/` |
| **Owner** | shared. An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- The answer to "Windows protected your PC" is four sections in this order: what the warning is, why it
  appears, exactly what to click, and what the app never does.
- **Quote the dialog** the user is looking at, so they can match it without reading the page.
- **State the real reason including the cost** - the build is not signed, a certificate is money, that was
  a choice. Never imply the warning is a mistake.
- **Never tell a user to weaken their protection.** More info -> Run anyway, or an exception for one
  folder. Nothing else.
- **Itemize every elevation** and name the single action that undoes all of it.
- "What the app never does" says the same thing as `docs/privacy.html`, the MSIX capability list and the
  Store data-safety answers.
- Lands in every authored locale in one edit, like any other user-facing surface.

**Status: adopted.** `docs/guides/install-trust.html` is the page, in the three locales the site
authors, and it is linked from the landing page's download band, the GitHub Release body, the guide hub, the install guide and all
five README locales. `cmd/filedo/fdsec_surfaces_test.go`
(`TestSurfaces_TheInstallTrustPageKeepsItsContract`) asserts the four sections in order, the quoted
dialogs, the stated cost, the absence of every instruction rule 4 forbids, the itemized elevations, and that
the fourth section still agrees with `docs/privacy.html`, the MSIX capability list and the Store
data-safety answers.

**When the build becomes signed, the page is updated, not deleted** (rule 8): users of older builds still
meet the warning, and the page is what search engines already point at.

**1.1 (clarifications and additions; a 1.0 page still conforms).** A translated page quotes the OS dialog string as Windows shows it in that language, else English with a bracketed gloss; it says which channels meet the warning; one undo is named per separately uninstallable entry and what survives it; a retired artifact is dated, not deleted. The page has not been re-read against these refinements in SP-0122; that re-read is open work, and until it is done the page is held to 1.0 by `TestSurfaces_TheInstallTrustPageKeepsItsContract`.

**1.2 (clarification and correction; a page that conforms to 1.1 conforms to 1.2), read 2026-10-07 (SP-0165).** Rule 4's list is closed, so the per-file "Unblock" checkbox is not a permitted instruction - it removes the mark of the web that SmartScreen keys on - and the four sections stay in the order of rule 1 (item L). No page, README, listing source or in-app string tells the user to Unblock a file (searched in all five languages), and the page test now forbids the word. The 1.1 re-read above is still open.

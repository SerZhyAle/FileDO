# INSTALL-TRUST

| | |
| --- | --- |
| **Id** | `INSTALL-TRUST` |
| **Version** | 1.0 (no wire carrier - a shipped user-facing document) |
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
authors, and it is linked from the landing page's download band, the guide hub, the install guide and all
five README locales. `cmd/filedo/fdsec_surfaces_test.go`
(`TestSurfaces_TheInstallTrustPageKeepsItsContract`) asserts the four sections in order, the quoted
dialogs, the stated cost, the absence of every instruction rule 4 forbids, the one undo action, and that
the fourth section still agrees with `docs/privacy.html`, the MSIX capability list and the Store
data-safety answers.

**When the build becomes signed, the page is updated, not deleted** (rule 8): users of older builds still
meet the warning, and the page is what search engines already point at.

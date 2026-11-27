# PAGE-STYLE

| | |
| --- | --- |
| **Id** | `PAGE-STYLE` |
| **Version** | 3.13, read 2026-11-17 (SP-0169 S6: sections 12, and the 3.3-3.13 amendments; the kit and its adapter duty are what the slice consumes); implemented partial 3.13 (3.9/3.11 per-locale landings shipped in v2611110217; 3.10 build-time release manifest `packaging/release-manifest.json` implemented in SP-0225; kit re-vendored 2026-11-17 to the 3.13 revision, SHA-256 `8d49a8f0..`, byte-identical in `docs/kit/` and `docs/help/`); rendered site re-read (header box, type, target profiles, group headers) open |
| **Role** | consumer - `docs/kit/sza-kit.css` and site styling |
| **Home** | shared contracts catalog, folder `product-web-pages/`, document `PAGE-STYLE.md` |
| **Owner** | sza.od.ua hub. Reference kit maintained in catalog |

## What this repository must do to stay conformant

- **Reference CSS kit.** `docs/kit/sza-kit.css` is the byte-identical vendored standard SZA palette tokens, typography (Manrope, Golos Text since the 3.13 pair), and container layouts; FileDO-specific styling lives in the page layer.
- **Kit 3.13 re-vendor (SP-0169 S6, 2026-11-17).** PAGE-STYLE 3.3 makes the revised kit itself the BUTTONS / UI-SIZING / COLLAPSIBLE-GROUP adapter, so `docs/kit/sza-kit.css` and the `docs/help` copy were re-vendored byte-identically to the 3.13 revision (SHA-256 `8d49a8f0a36369da62faf56a4dd9f026097f5aeba43b6d8f4f8c0edc56417e42`; a re-vendor is the same edit as the hash in `TestSurfaces_TheVendoredKitIsHeldByteForByte`). The page layer dropped what only compensated the pre-3.3 kit: the 44 px pins on covered controls (the kit draws the 32/48 profile targets), the light-theme `.seg` mute-ink override, the absolutist `.copybox .copy` centering, the `details.sec > summary` restyle and the mobile full-width hero buttons; the font link on every site page names the 3.13 pair. SITE-EXPERIENCE 0.5 item A keeps 44 px only for links and uncovered controls. A rendered re-read is owed at the release.
- **Pre-paint resolver.** Inline script prevents theme/language flash on load, accepts only `dark|light` and `ru|en|ua` from shared storage, and falls back to the documented system/browser defaults.
- **Responsive breakpoints.** Clean rendering at 360 px, 768 px, 1280 px without horizontal overflow; kit-covered controls take the kit's 32/48 profile targets, links and other uncovered controls keep >= 44 px under any pointer.
- **1.2 re-vendored the kit.** The catalog revised `reference/sza-kit.css` in the same amendment (print styles, the 44 px coarse-pointer rule for copy buttons and tool links, `--code-ink` on the demo block), so every consumer re-vendors: `docs/kit/sza-kit.css` was copied byte for byte on 2026-10-02 (SHA-256 `e544a6ce47160f827dc97379c3e4814d4aece763593c5a9420e681f1f4e4eb48`). Any later revision is a new copy, never a hand edit.
- **1.3 to 1.6, read 2026-10-07 (SP-0165).** The catalog's kit is the 1.3 file (14521 bytes, SHA-256 beginning `aea958f8`) and 1.4 owes a further revision; `docs/kit/sza-kit.css` stays the 1.2 file under a dated exception and is re-vendored once, with a rendered re-read at 360, 1280, 1920 and 2560 px, when the owed revision lands. Held in the page layer meanwhile: the copy button's done state is the localized word, the buttons and toggles are 44 px under every pointer, and the reduced-motion rule reaches `::before` and `::after` (1.5; `guides/guide.css` and the inline blocks). Open until the kit: the `nav.expand` disclosure marker and a labelled theme control (1.4). No page uses the 1.6 language picker.

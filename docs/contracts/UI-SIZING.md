# UI-SIZING

| | |
| --- | --- |
| **Id** | `UI-SIZING` |
| **Version** | 1.1 (no wire - component size, input profile and scale composition) |
| **Role** | consumer - every product-owned control of the GUI shell (`filedo_win_vb/`); migration ticket SP-0169 |
| **Home** | shared contracts catalog, folder `ui-sizing/` |
| **Owner** | shared (portfolio owner). An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **One resolver.** `UiTokens.vb` resolves the ordinary-size tokens of buttons, boolean options, group headers
  and navigation rows (32 / 12 / 16 / 8 / radius 4 / marker 16 / box 20); a covered component reads its
  numbers there, never a literal. Device pixels come from `Ui.Px`, so scale and DPI are composed once.
- **A stable input profile.** The pointer profile, chosen at start, never changed by an input event. It is the
  product's approved permanent variant (declared 2026-10-02, owner decision); no touch profile is exposed.
- **A declared smallest target.** 28 logical px, reached at the compact scale (0.875); the supported range is
  0.875 to 2.0 and an unknown scale clamps into it. Hit areas do not overlap.
- **Native controls keep platform sizing** where it cannot be customized (combo, edit, list); they are not
  stretched to a token.

**Gates.** `filedo_win.exe --selftest`, group `ui-contracts` - `ui-sizing:*` rows (profile, tokens, floor, scale,
clamp) and the scaled measurements of `buttons:scale`.

**Status: not conformant - migration in progress (SP-0169, final deadline 2026-12-31).** Built so far (slice S1,
2026-10-21): the resolver, the profile and the buttons and group headers that read it. Owed: the rest of the
covered components (options, navigation rows); a user-visible size selector is a product decision not taken (D1);
the font side of the interface scale; the showcase and measurements at compact to huge in five languages (S4).

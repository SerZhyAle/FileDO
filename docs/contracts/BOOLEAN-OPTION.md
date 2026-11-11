# BOOLEAN-OPTION

| | |
| --- | --- |
| **Id** | `BOOLEAN-OPTION` |
| **Version** | 2.0 (no wire - observable boolean option layout and interaction) |
| **Role** | consumer - every boolean option of the GUI shell and the Disk Manager (`BooleanOption`); migration ticket SP-0169 |
| **Home** | shared contracts catalog, folder `boolean-option/` |
| **Owner** | shared (portfolio owner). An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

A leading visible vector checkbox, a heading (optional ICON-SET glyph, short name, optional help button) and a
useful explanation under it; box, caption and glyph toggle once on every platform, explanation and help never do;
one named checkbox whose description is the explanation; short hover/focus help and activated full help from one
authored source (the SP-0168 `help_*` keys); a disabled option says why. Stored values, keys and defaults do not
change.

**Gates.** `--selftest` group `ui-contracts`: `option:*` (tokens, anatomy, heading, no empty row, wrap, activation,
right to left, disabled path and reason, accessibility, contrast in both palettes, no native check box left in the
shell or the Disk Manager, every Settings option explained) and `windows-ui:boolean-leading:*`.

**Status: not conformant - migration in progress (SP-0169, deadline 2026-12-31).** Slice S2 (2026-10-29) built the
shared `BooleanOption` component and put it on every boolean option of the shell and the Disk Manager; the Settings
rows carry their description as the explanation and the sealed-disk sharing option says why it is locked. Owed:
local option help (hover summary and activated text from one source), the showcase with captures (S4), and a
measured run of the gate rows above.

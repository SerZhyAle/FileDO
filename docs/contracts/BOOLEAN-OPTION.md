# BOOLEAN-OPTION

| | |
| --- | --- |
| **Id** | `BOOLEAN-OPTION` |
| **Version** | 2.0 (no wire - observable boolean option layout and interaction) |
| **Role** | consumer - every boolean option of the GUI shell (36 `CheckBox` sites); migration ticket SP-0169 |
| **Home** | shared contracts catalog, folder `boolean-option/` |
| **Owner** | shared (portfolio owner). An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

A leading visible vector checkbox, a heading (optional ICON-SET glyph, short name, optional help button) and a
useful explanation under it; box, caption and glyph toggle once on every platform, explanation and help never do;
one named checkbox whose description is the explanation; short hover/focus help and activated full help from one
authored source (the SP-0168 `help_*` keys); a disabled option says why. Stored values, keys and defaults do not
change.

**Gates.** None yet: the component has not been built.

**Status: not implemented - nothing claimed (SP-0169 slice S2, target 2026-11-04).** The shell's boolean options
are native check boxes (a 28 px floor from `Ui.HitTargetFloor`), with an explanation only on the Settings page.

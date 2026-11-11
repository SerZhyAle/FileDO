# BUTTONS

| | |
| --- | --- |
| **Id** | `BUTTONS` |
| **Version** | 2.0 (no wire - observable button appearance, layout and interaction) |
| **Role** | consumer - `GlyphButton` and every dialog answer of the GUI shell; migration ticket SP-0169 |
| **Home** | shared contracts catalog, folder `buttons/` |
| **Owner** | shared (portfolio owner). An amendment is proposed in the catalog, not decided here |

## What this repository must do to stay conformant

- **Tokens, shape, content width** (section 3): padding 12, glyph 16, gap 8, ordinary height 32, radius 4 - from
  `UiTokens`; the width is content plus padding, never the container's.
- **A caption is never ellipsized or clipped**: it wraps and the button grows; every button in one visual row
  takes the height of that row's tallest (`Ui.EqualizeRowHeights`).
- **Neutral answers** (3.5): a dialog answer keeps the ordinary face; the hue is on the glyph alone (state.ok for
  an affirmative answer, state.warning for a caution one); Cancel, No and a plain Close are mono; only an
  irreversible confirmation's acting button is danger-filled.
- **Answer order** (4.4): Cancel, any middle answer, then the final affirmative - `ShellDialog.AnswerOrder`
  decides the left-to-right place and the Tab order; the indices callers pass are unchanged.
- **Safe default** (4.5): the safe answer holds the default focus; Escape and the close box give the no-action
  answer (held by `dialog:destructive-default-safe:*`).
- **Policy recorded:** a dialog answer carries its verb's glyph (OK/Yes/No-style answers may be text-only); every
  other command carries its ICON-SET meaning where one exists.
- **Reserved state width** (3.3): a button whose caption changes with its state names the other states in
  `ReservedCaptionKeys` and is as wide as its widest state in every state.

**Gates.** `--selftest` group `ui-contracts` (`buttons:*` including `buttons:state-width` and
`buttons:page-row-heights`, `dialog:answer-order:*`) and the `contrast:*:answer-*` rows on the normal, hover and
pressed faces.

**Status: not conformant - migration in progress (SP-0169, deadline 2026-12-31).** Slice S1 (2026-10-21) built the
token geometry, wrapping, neutral faces and answer order; slice S2 (2026-10-29) the reserved state width of Stop and
the output toggle, and a row-height check over every page flow. Owed: an overflow policy, a command id per command,
the showcase with captures (S4), and a measured run of the gate rows.

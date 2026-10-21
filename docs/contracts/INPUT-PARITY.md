# INPUT-PARITY

| | |
| --- | --- |
| **Id** | `INPUT-PARITY` |
| **Version** | 0.4, read 2026-10-21 (SP-0170); implemented partial 0.3; F6 and the Disk Manager help menu are SP-0171 W11, draft; partial adoption |
| **Role** | consumer - GUI shell and Disk Manager, keyboard and mouse |
| **Home** | shared contracts catalog, folder `input-controls/`, document `README.md` |
| **Owner** | shared; keyboard and mouse steward CyrFlip |

## What this repository must do to stay conformant

- Pointer actions have keyboard routes, focus stays visible, and Cancel never destroys user work.
- Use the keyboard and mouse bindings for supported actions; media, remote and gamepad controls have no FileDO surface.
- `DiskShortcuts.All` drives Disk Manager handlers, help and tooltips. Menu/Shift+F10, Ctrl+F, Enter and Escape have explicit mappings; native controls provide Tab, arrows and paging.
- The catalog exception records the missing F6 pane route. Complete keyboard-only and focus verification per screen before claiming full conformance; automated selftests alone do not prove it.

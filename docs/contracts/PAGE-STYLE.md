# PAGE-STYLE

| | |
| --- | --- |
| **Id** | `PAGE-STYLE` |
| **Version** | 1.1 |
| **Role** | consumer - `docs/kit/sza-kit.css` and site styling |
| **Home** | shared contracts catalog, folder `product-web-pages/`, document `PAGE-STYLE.md` |
| **Owner** | sza.od.ua hub. Reference kit maintained in catalog |

## What this repository must do to stay conformant

- **Reference CSS kit.** `docs/kit/sza-kit.css` is the byte-identical vendored standard SZA palette tokens, typography (Outfit, Plus Jakarta Sans), and container layouts; FileDO-specific styling lives in the page layer.
- **Pre-paint resolver.** Inline script prevents theme/language flash on load, accepts only `dark|light` and `ru|en|ua` from shared storage, and falls back to the documented system/browser defaults.
- **Responsive breakpoints.** Clean rendering at 360 px, 768 px, 1280 px without horizontal overflow; touch targets >= 44 px.

# Fonts

Served by Ballast itself, so that building the front end needs no access to a third party.

| File | Font | Source | Licence |
|---|---|---|---|
| `dm-sans-latin.woff2` | DM Sans, variable, weights 400 to 500, Latin subset | Google Fonts, `fonts.gstatic.com/s/dmsans/v17` | SIL Open Font License 1.1 |
| `jetbrains-mono-latin.woff2` | JetBrains Mono, variable, weights 400 to 500, Latin subset | Google Fonts, `fonts.gstatic.com/s/jetbrainsmono/v24` | SIL Open Font License 1.1 |

Both licences allow redistribution with the application. To change a font, replace the file and
keep the two weights: `npm run lint` refuses any other.

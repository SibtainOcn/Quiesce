# Translating Quiesce

Quiesce ships English and Spanish. Adding a language takes one file and no
Go code.

## How the language is chosen

1. Quiesce reads the Windows display language at startup.
2. If a `LANGUAGE=` line exists in `cleaner_config.dat`, it wins.
3. Anything with no translation falls back to English.

Users change it in the app: **F** for settings, then **L**, pick by number,
then **E** to save. That writes the `LANGUAGE=` line for them. It can also be
set by hand in `cleaner_config.dat`, next to `qc.exe`:

```
LANGUAGE=es
```

## Adding a language

1. Copy `locales/active.en.toml` to `locales/active.<code>.toml`, where
   `<code>` is the two-letter [ISO 639-1] code - `fr`, `de`, `pt`, `ar`.
2. Translate the value on the right of each `=`. Leave the key on the left
   alone.
3. Add your language's own name to `languageNames` in `lang.go`, so it
   appears in the in-app picker:

   ```go
   var languageNames = map[string]string{
       "en": "English",
       "es": "Español",
       "fr": "Français", // <- yours
   }
   ```

   Write it the way speakers of that language write it, not translated into
   English - someone looking for their language needs to recognise it.
4. Add the language to `primaryLangCode` in `lang.go` so Windows auto-detect
   finds it. The [Windows LANGID list] gives the number; use the low 10 bits.
5. Run the tests: `go test ./...`
6. Build: `go build -o qc.exe`

The file is embedded into the binary automatically - there is nothing to
register and no build step to run.

## Rules

**Keep the format verbs.** `%s`, `%d`, `%.1f` are placeholders the program
fills in. Keep every one, and keep them in the same order unless your
language genuinely needs a different order.

```toml
# English
"summary.total" = "Total items cleaned : %d"

# Correct
"summary.total" = "Elementos limpiados : %d"

# Wrong - the number has nowhere to go
"summary.total" = "Elementos limpiados"
```

`%%` is a literal percent sign, not a placeholder.

**Do not add trailing spaces.** Column alignment is calculated while the
program runs. Typed spaces would be added on top of it and push the column
out of line.

**Leave Windows terms in English.** Windows itself does not translate
`Prefetch`, `DNS`, `RAM`, `Temp`, `cleanmgr` or `NTSTATUS`. Translating them
makes it harder for someone to match Quiesce's output against what Windows
shows them.

**Write accents properly.** The console is switched to UTF-8, so `á`, `ñ`
and `¿` display correctly. Never strip accents to work around a display
problem - report it instead.

**Keep it short.** This is a console UI. A label two or three words longer
than the English will still align, but it makes the screen wide.

## What the tests check

`go test ./...` fails if:

- a key is missing from your file, or has one English does not have
- your format verbs do not match English
- a value has trailing whitespace
- any menu or summary column stops lining up in your language

If those pass, your translation is safe to merge.

## Submitting

Open a pull request with your `locales/active.<code>.toml` and the two
one-line additions to `lang.go` (`languageNames` and `primaryLangCode`).
CI runs the checks above automatically.

[ISO 639-1]: https://en.wikipedia.org/wiki/List_of_ISO_639_language_codes
[Windows LANGID list]: https://learn.microsoft.com/en-us/windows/win32/intl/language-identifier-constants-and-strings

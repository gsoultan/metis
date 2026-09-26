# Language

The interface can be shown in more than one language. English is the source of
truth; every other catalogue is a translation of it.

## Using it

```tsx
import { useTranslation } from '../i18n/context';

const { t } = useTranslation();
t('inbox.title');                          // "Task Inbox"
t('inbox.subtitle', { name: user.name });  // "Manage and complete tasks for Alice."
t('inbox.nothingWaiting', { count: 3 });   // "3 tasks waiting"
```

Keys are `area.thing`, flat and sorted. Add the English text to
`catalogues/en.ts` first — it is the wording the product uses, so it is written
there and translated afterwards, never the other way round.

## What it supports, and what it does not

Interpolation (`{name}`) and plurals
(`{count, plural, one {# task} other {# tasks}}`), and nothing else. Plural
categories come from `Intl.PluralRules`, so a language with four of them —
Polish — is handled by the platform rather than by a rule this repository would
get wrong. `#` means the count, and only inside a plural form, so an ordinary
"Ticket #4471" survives.

Written here rather than taken from a library because the first-paint budget is
enforced by the build and a full ICU runtime is a meaningful share of it, and
because this is decidable logic: given a catalogue, a key and some values there
is one right answer, which is the kind of thing this repository puts in a tested
module. See `translate.ts` and its tests.

## A missing key shows the key

Not a blank, and not English. A blank space where a word should be is a bug
nobody can see in a screenshot, and an English fallback hides a missing
translation from the only person who would notice it. `inbox.titel` on screen is
ugly, which is the point.

The one exception is a catalogue that fails to *load* — a network error leaves
English in place, because the wrong language beats a page of identifiers.

## What is translated so far

The shell: navigation, the language menu itself, and the offline and update
messages. The page headings, the Dashboard's figures, the getting-started card
and Help's checklist and glossary. The glossary's steps keep the palette's
names, which are still English, so a step can be looked up by the name seen on
it. The Roles view on the Platform access page, whose role names and their
sentences come from `domain/roles.ts` and the actions under each from the
server, both still English. **Everything else is still hardcoded English** — the rest of the pages,
forms, designer and decision editor, which is the large majority of the strings.

That is a deliberate stopping point rather than a claim of completeness. The
machinery is in place, proven by a second language that is not a copy of English
(Indonesian has no plural inflection, so it exercises the `Intl` path), and the
remaining work is mechanical: move a string into `en.ts`, replace it with
`t('key')`, translate.

`locales.test.ts` asserts that every key a translation defines exists in
English, so a typo in a translated key fails a test rather than appearing on
screen.

## Adding a language

Add a catalogue under `catalogues/`, then a row in `LOCALES` with its tag and
its **endonym** — the name the language uses for itself, because somebody
looking for their own language is looking for the word they use. The catalogue
is fetched only when chosen, so a language nobody selects costs nothing.

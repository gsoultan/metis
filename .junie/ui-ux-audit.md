# Metis BPM — UI/UX Audit

Evidence-based audit of `ui/` as it stands. Every claim below was measured, not
estimated; the command that produced each number is given so you can re-run it.

**Reprioritization note** (required by `AGENTS.md` §2.1 `pm`): Phase 5 (UI/UX +
stack upgrade) is being executed ahead of Phase 2 (FEEL) and Phase 3
(integration platform) at the product owner's direction. P0/P1 security work is
already complete, so this does not jump a security gate.

---

## The one-sentence summary

The UI is **visually competent and functionally hollow**: it looks like a
finished product, and that is the problem — it makes promises (metrics,
settings, help, search) that the code does not keep.

A user cannot tell a real number from an invented one, and that is the single
most damaging thing a business-process product can do.

---

## Part 1 — Findings that break trust

These are not styling issues. They are the ones I would fix before anyone sees
a demo.

### 1.1 The dashboard displays invented business metrics

`src/pages/Dashboard.tsx`:

```tsx
<StatCard title="Active Instances"  value={stats.active_instances} trend="+12%" />
<StatCard title="Task Completion"   value={`${completionRate}%`}   trend="+5%"  />
<StatCard title="SLA Compliance"    value="94%"                    trend="+2%"  />
```

- `trend` is a **hardcoded string** on all three cards, rendered next to a green
  upward arrow and the words "vs last month".
  `grep -rin "trend\|last_month\|previous_period" server --include="*.go"` →
  **no matches**. The backend computes no trend of any kind.
- **SLA Compliance is the literal string `"94%"`.** It is not derived from
  anything. (There *is* a real `ListBreachedSLAs` in
  `server/domains/services/impl/heatmap_service.go` — so the honest number is
  available and simply not used.)

**Why this is the worst finding.** Every other issue in this document costs a
user time. This one costs them *correctness*. Someone will screenshot "SLA
Compliance 94% ▲+2%" into a steering-committee deck. In a regulated industry
that is a finding, not a bug.

**Principle — never render a number you cannot defend.** If the data does not
exist: show the empty state, or do not show the card. A missing metric is
honest. An invented one is not.

### 1.2 Loading states are absent, and the placeholder looks like data

Measured across `src/pages/`:

| | |
| :-- | :-- |
| Pages that render a skeleton while loading | **3 of 18** |
| Pages with no loading treatment at all | **15** |

The Dashboard is the worst case, because it does not merely show nothing — it
shows *zeros*:

```tsx
const stats = statsData?.stats || {
  active_instances: 0, completed_instances: 0, failed_instances: 0, ...
};
```

Before the request resolves, the user sees a fully rendered dashboard reporting
zero active instances. That is indistinguishable from a real, quiet system.

**Principle — the three states are not optional.** Every data surface has
**loading**, **empty**, and **error**. Defaulting to `0` collapses "we don't
know yet" into "we know, and it's none", which are opposite meanings.

### 1.3 Nine controls do nothing

Verified with a multi-line-aware scan for `<Button>` with no `onClick`,
`component`, `href`, or `type="submit"`:

| File | Label |
| :-- | :-- |
| `pages/Settings.tsx` | **"Save All Settings"** |
| `pages/Settings.tsx` | **"Reset to Defaults"** |
| `pages/Settings.tsx` | "Enable", "Clear" |
| `pages/Profile.tsx` | "Change Password" |
| `pages/Dashboard.tsx` | "View All Logs", "Use Template" |
| `containers/MainLayout.tsx` | "Contact Support" |
| `components/NotificationCenter.tsx` | "View all notification history" |

The Settings page has no working save **and** no working reset: it is entirely
decorative. A user will change a setting, click Save, see nothing happen, and
reasonably conclude the product is broken.

**Principle — a disabled control teaches; a dead control lies.** If a feature
isn't built, either don't render it, or render it disabled with a tooltip
saying why. Silence on click is the one option that destroys confidence.

### 1.4 The help drawer advertises features that don't exist

`containers/MainLayout.tsx`:

- *"Use **Cmd + K** anywhere to search for nodes, tasks, and documentation."*
  Spotlight exists **only inside the process designer**
  (`components/DesignerModals.tsx`). Everywhere else the shortcut does nothing.
- Four documentation buttons — "BPMN 2.0 Reference", "JavaScript Scripting",
  "API Guide", "Contact Support" — none navigate anywhere.
- *"Our support team is available Mon-Fri, 9am-5pm."* There is no support team
  configured anywhere in the product.

**Principle — help is the last place you can afford to be wrong.** A user opens
help because they are already stuck. Failing them there converts confusion into
distrust.

---

## Part 2 — Findings that block enterprise adoption

### 2.1 Accessibility is effectively zero

```
ActionIcon (icon-only buttons): 66 total, 0 with an accessible name
Files containing any aria-label or role: 1 of 78 .tsx files
```

Sixty-six controls announce as an unlabelled "button" to a screen reader. There
is no way to know which one deletes a process definition.

This is not a nicety. WCAG 2.1 AA conformance and a VPAT are standard line items
in enterprise and public-sector procurement. The honest answer today is "no",
and that ends conversations before the product is evaluated on merit.

**Principle — every control needs an accessible name, and every flow needs a
keyboard path.** For an icon-only button that is one attribute:
`aria-label="Delete process definition"`.

### 2.2 No internationalization

No i18n library, no message catalogue; every user-facing string is hardcoded
English inside JSX. Retrofitting this across 78 components later costs several
times what extracting strings during the refactor costs now — which is precisely
why it belongs in this phase and not a later one.

### 2.3 The typography that was designed is not the typography that ships

Both `index.css` and the Mantine theme declare `font-family: Inter, …`.
**Inter is never loaded** — no `@font-face`, no stylesheet link, not a
dependency. Every user sees their platform's default sans-serif instead.

The design intent silently degrades, and it degrades *differently* on macOS,
Windows and Linux, so the product looks inconsistent across the people
evaluating it.

### 2.4 Responsive coverage is partial and the shell will break first

- 15 of 34 page/component files use any responsive prop.
- 41 files contain hardcoded pixel widths.
- The header packs two 200px selects, a project chip, notifications, help and a
  user menu into one row with no `hiddenFrom`. On a laptop at 1280px with both
  selects populated this is already tight; below that it overflows.

---

## Part 3 — Findings that make it feel unprofessional

### 3.1 It still looks like a scaffold

`index.html` is unchanged from `npm create vite`:

```html
<title>ui</title>
<link rel="icon" type="image/svg+xml" href="/vite.svg" />
```

The browser tab, every bookmark, and every history entry says **"ui"**, with the
Vite logo beside it. No description meta, no theme-color, no apple-touch-icon.

This is the cheapest possible fix and among the most visible.

### 3.2 Every card claims to be clickable

```tsx
Card: Card.extend({
  defaultProps: { withBorder: true, padding: 'xl', radius: 'lg', shadow: 'md' },
  styles: { root: {
    transition: 'transform 200ms ease, shadow 200ms ease',
    '&:hover': { transform: 'translateY(-2px)', boxShadow: 'var(--mantine-shadow-lg)' },
  }},
}),
```

Applied to **every** Card, including static ones. Lifting on hover is the
universal signal for "this is interactive"; using it on a read-only panel
teaches users to click things that do nothing.

(Also: `transition: … shadow 200ms` names a property that does not exist. The
CSS property is `box-shadow`, so that half of the transition never runs — the
shadow snaps while the transform animates.)

**Principle — motion is a promise.** Reserve hover elevation for surfaces that
respond to a click.

### 3.3 Everything shouts

`headings.fontWeight: '800'`, plus `fw={800}` on section titles, `fw={700}` on
labels, uppercase + letter-spacing on table headers and stat labels, and a
gradient-filled wordmark.

When everything is emphasised, nothing is. Enterprise tools earn seriousness
through restraint: 600 for headings, 500–600 for labels, and one accent colour
used sparingly.

### 3.4 An ordinary empty state is styled as an error

```tsx
<Badge color="red" leftSection={<AlertCircle/>}>No Projects Found</Badge>
```

Red with a warning icon means *something is broken*. "You haven't created a
project yet" is not broken — it is the expected state of a new account, and it
is an opportunity to offer the next action.

**Principle — match severity to reality**, and make empty states do work: say
what this is for, and give one primary action.

### 3.5 Technical vocabulary reaches the end user

`{user?.role}` renders the raw value — **`ADMIN`**. The project's own
`.junie/guidelines.md` §5 mandates human-readable language ("The manager
approved the request", not `Task_Approved`); the shell violates it in the first
thing a user sees after their own name.

### 3.6 Dark mode is hand-rolled at every call site

```tsx
bg={theme === 'dark' ? 'dark.7' : 'white'}
style={{ borderBottom: `1px solid ${theme === 'dark' ? 'var(--mantine-color-dark-4)' : 'var(--mantine-color-gray-2)'}` }}
```

This ternary is repeated throughout. Mantine solves it with the `light-dark()`
CSS function — which requires `postcss-preset-mantine`, **which is not
installed**. So the modern approach is unavailable and the manual one is
guaranteed to drift.

`forceColorScheme` also *overrides* the user's OS preference rather than
defaulting to it.

---

## Part 4 — Architecture behind the symptoms

The surface problems above share four root causes. Fixing symptoms without
these means fixing them again.

| Root cause | Evidence | Consequence |
| :-- | :-- | :-- |
| **No shared state components** | No `EmptyState`, `LoadingState`, or `ErrorState` in `src/components/` | 18 pages each decide independently; 15 chose "nothing" |
| **Design tokens live in a route file** | `createTheme` inside `routes/__root.tsx` | Tokens aren't importable, so components hardcode values |
| **No layout primitives** | 41 files with hardcoded pixel widths | Every page re-invents spacing; nothing reflows consistently |
| **God components** | `TaskInbox` 905 lines, `DecisionEditor` 709, `Connectors` 662, `useProcessDesigner` 630 | Fetching, mapping and rendering interleaved — untestable, and the reason the designer's two `setState`-in-effect bugs can't be safely fixed |

Only **1 frontend test** exists across ~16k lines, which is why none of the
above was caught.

---

## Part 5 — What I propose to do, in order

Ordered so that no work is done twice.

### Stage A — Upgrade the platform first
Refactoring on Mantine 8 and then migrating to 9 would touch every component
twice. Upgrade first, each step green and independently revertible:

`Vite 8` → `postcss-preset-mantine` + drop dead Emotion → `Mantine 9` →
`TanStack Router/Query` → `Tailwind 4` → `TypeScript 7` → `TanStack Form`

### Stage B — Foundations
1. **Honesty pass** — delete every invented metric and dead control. Highest
   trust-per-line-changed in the entire plan.
2. **Design tokens** out of the route file into `src/theme/`, with a restrained
   type scale and one accent.
3. **State components** — `EmptyState`, `LoadingState`, `ErrorState`,
   `PageShell` — then adopt them across all 18 pages.
4. **`index.html`** — real title, favicon, description, theme-color.
5. **Load Inter properly**, or drop it from the stack and own the system font.

### Stage C — Accessibility and language
6. Accessible names on all 66 icon-only controls; keyboard paths; `@axe-core/react`
   in CI so it cannot regress.
7. i18n extraction *during* the component work, not after.

### Stage D — The four hero surfaces
8. **Task Inbox** — the daily driver. Split the 905-line component, virtualize,
   business language, bulk actions, saved views.
9. **Process Designer** — validation in-canvas, guided mode, real Cmd+K.
10. **Decision Editor** — spreadsheet-grade grid, inline FEEL with autocomplete.
11. **Monitoring** — render the execution path on the diagram (the backend
    already returns the frequency data; nothing displays it).

---

## How to re-run this audit

```bash
cd ui
# loading-state coverage
for f in src/pages/*.tsx; do printf "%-22s %s\n" "$(basename $f)" "$(grep -c Skeleton $f)"; done
# icon-only buttons without an accessible name
grep -c "<ActionIcon" -r src --include="*.tsx"
# dead controls
grep -rn "<Button" src --include="*.tsx" -A3 | grep -B1 -A3 ">" | grep -v onClick
```

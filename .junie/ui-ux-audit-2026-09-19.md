# Metis BPM — UI/UX Audit, 2026-09-19

Second audit. Supersedes the findings in [`ui-ux-audit.md`](ui-ux-audit.md) where they
conflict — that document predates the Phase 5 work and **most of its findings are now
fixed**. What follows was produced by running the app (`make dev`, seeded sample data,
PostgreSQL) and driving every route in a real browser, not by reading source.

Reviewed against `v0.3.0-1-g130d896` at three levels, as asked: interaction design,
frontend engineering, and a first-time non-technical user trying to get work done.

---

## What the previous audit flagged that is now genuinely fixed

Re-ran its own verification commands. Do not spend time on these again:

| Old finding | State now |
| :-- | :-- |
| Invented metrics (`trend="+12%"`, `SLA "94%"`) | **Fixed.** The `trend` prop was removed outright, with a comment saying why. |
| Dead controls (Save All Settings, Change Password, …) | **Fixed.** A `ComingSoonButton` renders them `[disabled]` with a reason; Change Password is now real. |
| `<title>ui</title>`, Vite favicon, no meta | **Fixed.** Real title, description, favicon, apple-touch-icon, per-scheme theme-color, `<noscript>`. |
| Inter declared but never loaded | **Fixed** (but see F19 — it now loads from a third-party CDN). |
| 66 icon-only controls, 0 accessible names | **Mostly fixed.** 78 `ActionIcon`, 119 `aria-label`. Header and nav are fully named. |
| No shared state components | **Fixed.** `src/components/state/` (`EmptyState`, `LoadingState`, `ErrorState`, `DataView`) adopted by 13 of 19 pages. |
| Manual `theme === 'dark'` ternaries everywhere | **Mostly fixed.** 23 `light-dark()` uses vs 5 remaining ternaries. |
| Only 1 frontend test | **Fixed.** 517 tests across 55 files. |

Two regressions against that audit: the god components got **bigger**, not smaller
(`DecisionEditor` 709 → 1228 lines, `TaskInbox` 905 → 1030, `useProcessDesigner` 630 →
799), and axe-core now runs in dev and reports real violations that nobody has cleared.

---

## P0 — These stop somebody doing the job

### F1. The inbox tells you there is no work while showing a badge that says there is

`src/pages/TaskInbox.tsx`. Landing on **My Inbox** — the daily driver — the default
"Assigned to Me" tab renders the empty state:

> **You're all caught up**
> Nothing needs your attention right now.

directly beneath a tab reading **"Available to Claim `12`"**.

Twelve tasks are waiting. The screen's largest text says there are none. A user who
believes the app closes it.

**Fix.** An empty state must know what is next to it. When "Assigned to Me" is empty and
claimable work exists, say so and link to it: *"Nothing is assigned to you. 12 tasks are
waiting to be claimed →"*. Better still, open on the tab that has work.

### F2. The dashboard says nothing has failed while four instances are stuck

`src/pages/Dashboard.tsx:226`. The **Needs Attention** card reads `0`, coloured green,
captioned **"Nothing has failed"**. The Instances page, same data, same moment, shows
**"4 Need attention"** with red markers on four `New supplier check` runs stuck at
`Lookup`.

Root cause: the card binds `stats.failedInstances` (`Dashboard.tsx:127`) — instances whose
*status* is FAILED. A BPMN instance with an exhausted job does not go FAILED; it stays
**RUNNING** and raises an incident. So the counter measures a state this engine almost
never produces, and the reassuring copy is emitted on the strength of it.

This is the operator's first screen, and it is confidently wrong in the safe direction.

**Fix.** Count incidents, not instance status — `InstanceList` already asks the right
question. Until then the card must not say "Nothing has failed."

### F3. You cannot complete a task on a phone

At 375px the task table renders two columns — Task Name and Assignee. Status, Variables,
Due Date and **Actions** are gone, so the **Complete** button is unreachable. There is no
horizontal-scroll affordance to suggest the columns exist.

The page does not overflow the viewport (`scrollWidth == innerWidth` at 375 and 1280), so
the shell is responsive; the table is not. An approver on a phone — the single most likely
mobile user of a BPM tool — can read their queue and do nothing about it.

**Fix.** Below `sm`, drop the table for a card list carrying the primary action.

### F4. A non-technical user cannot start a process model

`/designer` opens on an empty grid with no instruction. The element palette is **hidden**
behind a "Components" button in the top-right toolbar, grouped with Import and Export so
it reads as a view toggle rather than the primary tool. Camunda Modeler and bpmn.io both
keep the palette open on the left, permanently.

Also on this screen:

- **"Deploy Model"** is the filled primary button, on a process that is empty and invalid.
- The validation card (*"This process is empty."*) is a small floating panel at the
  bottom-left, overlapping the unlabeled `+` button. It names the problem and offers no
  next step.
- A red **delete** immediately beside the grid toggle. (The toolbar icons do carry
  `aria-label`s — an earlier draft of this document said they did not, which was wrong.
  What was wrong is that the delete button's accessible name said "Delete" while its
  tooltip said "Clear Canvas".)
- `Key: new_process` — an internal identifier in the page's second line.
- A **"React Flow"** watermark sits in the bottom-right of the canvas.

`roadmap.md` §7 lists "Guided Process Designer Wizard — step-by-step mode for
non-technical users, template gallery" as 🔴 High Priority. It is still not built, and the
dashboard's template gallery is three disabled buttons (F14).

**This is where a business user gives up.** It is the highest-value UX work in the product.

---

## P1 — Actively misleading

### F5. Every unassigned task claims to be assigned to "Current User"

The All Tasks table prints **"Current User"** in the Assignee column for all 12 rows —
rows whose Status is `AVAILABLE`, i.e. unassigned. It is a placeholder string rendered as
data. It also contradicts F1: the inbox says nothing is assigned to me; this page says all
twelve are.

Absent data must render as absent — "Unassigned", dimmed — never as a stand-in name.

### F6. Three screens are named one thing in the nav and another in the page

| Sidebar | Page `<h1>` | Subtitle claims |
| :-- | :-- | :-- |
| All Tasks | **My Tasks** | "your assigned tasks" (they are unassigned) |
| Processes | **Models** | — |
| Decisions | **Models** (a tab) | two nav items, one page, one highlighted |

A user cannot build a mental model of an app that renames its own screens between the
click and the arrival.

### F7. The language switcher translates the navigation and nothing else

Switching to **Bahasa Indonesia** translates the 17 sidebar labels (`Dasbor`, `Kotak Masuk
Saya`, `Semua Tugas`, `Proses`, `Keputusan`, …) and leaves every page body in English. The
result is one screen where the nav says **Dasbor** and the `<h1>` beside it says
**Dashboard**.

No third-party i18n library is installed, but this is not a stub: `src/i18n/` is a real,
tested translation layer — catalogues, ICU plurals, interpolation, a provider, and a
`locales.test.ts` that enforces key parity between languages. It was written deliberately
rather than taken from a library, to keep the first-paint budget.

The gap is adoption, and it is total: **no page component called `t()` at all.** Only
`components/shell/Sidebar.tsx` did. The catalogue even carried unused `login.*` and
`inbox.*` keys nothing consumed. So a complete translation system was wired to exactly one
component, which is worse than having no switcher: it advertises Indonesian support, and
an Indonesian user hits a wall of English one pixel to the right of the translated menu.

**Fix.** Either remove the switcher until real i18n lands, or scope it honestly. Shipping
`execution-plan.md`'s i18n item is the real answer.

### F8. The Variables column is raw database content

Each task row carries six badges of untransformed key/value pairs, truncated mid-word:

```
AMOUNT: 1750     APPROVALLEVEL: DIR…   APPROVER: FINANCE…
CURRENCY: GBP    DESCRIPTION: EXPEN…   SUBMITTEDBY: ALICE
```

`AMOUNT: 1750` and `CURRENCY: GBP` are separate badges where a person reads **£1,750**.
`.junie/guidelines.md` §5 requires human-readable language; this is the schema shown to the
approver. It is also why each row is ~137px tall, so twelve tasks fill 2,000px.

**Fix.** Two or three composed, formatted facts per row — `£1,750 · Alice · Expenses` —
and the rest behind the row expander.

### F9. The overdue badge is unreadable, twice over

The Due Date column renders a red pill clipped to **`OVE…`**. axe measures it at **3.58:1**
contrast (white on `#e8590c`) at **9px**, against a WCAG AA requirement of 4.5:1.

Urgency is the one thing a task queue must communicate, and this communicates it in three
clipped characters at a contrast ratio that fails conformance.

### F10. Version badges clip the only fact that matters

The Models table shows `V1 LI…` and `V4 STA…`. Which version is *live* versus *staged* is
the single most important attribute of a deployed process, and it is truncated. The colour
coding (blue/orange) is unexplained.

### F11. The dashboard counts process models differently from the models page

Dashboard: **"PROCESS MODELS 8"**. The Models page lists **2** rows. Both are defensible
(8 definitions = 2 processes × 4 versions), and a user reading them an hour apart has no
way to reconcile them.

### F12. The login failure shows a wrapped Go error, twice

```
Authentication Failed
authentication failed: invalid credentials
```

The title says it; the body repeats it in lowercase in the server's own
`fmt.Errorf("authentication failed: %w", …)` phrasing. There is also no forgot-password
path, and recovery genuinely requires shell access to run `metis --reset-password` — which
the screen does not say.

---

## P2 — Hollow surface and polish

- **F13. Settings is two-thirds unavailable.** Of six controls, four are disabled:
  Two-Factor Authentication, API Keys, Clear Cache, Reset to Defaults. Only theme and
  Expert Mode work. A red-bordered **"Danger Zone"** contains exactly one disabled button.
  (2FA being absent is also a security gap for a product approving money — see
  [`security-plan.md`](security-plan.md).)
- **F14. Starter Templates.** A section badged **RECOMMENDED** offering three cards whose
  every button is disabled. Prime dashboard real estate that can do nothing. Do not render
  it until templates exist.
- **F15. "Batch Complete" is the hero action on financial approvals.** The filled primary
  button on the task list bulk-completes approvals. Per-item deliberation is the point of
  an approval step.
- **F16. 13 unlabeled form controls on `/tasks`.** All 106 console errors on that route are
  axe violations, not crashes — but "no explicit `<label>`", "aria-label does not exist or
  is empty" and "default semantics not overridden" repeat 13 times each. axe-core is
  already wired into dev; nothing is acting on it. Put it in CI and the count cannot grow.
- **F17. The Business Timeline card is a fixed height.** Three events, ~350px of dead white
  space below them.
- **F18. The login footer overlaps the feature grid.** `© 2026 Metis BPM` collides with the
  "Decisions … FEEL engine" text at the bottom-left.
- **F19. Inter now loads from `fonts.googleapis.com`.** The fix for the unloaded font
  introduced a runtime dependency on a third-party CDN. For a self-hosted, distroless,
  air-gap-capable product this fails closed to the platform font with no notice, and
  embedding Google Fonts has been held to breach GDPR in German courts — a live objection
  for the EU enterprise buyer this product targets. Self-host with `@fontsource/inter`.

---

## One backend bug found while driving the UI

**`--reset-password` starts a job worker.** Running it logs `Job worker started
workers=10`, `SSE fan-out started` and `Rate limits are pooled across replicas` under a
*different* replica ID from the running server — two engines, briefly, against one
database.

`internal/app/app.go:311` returns before transports, and its comment says the reset "runs
against the configured database and then exits, without opening a port". True about the
port. The cause is one step earlier: `setupService` (step 3, `app.go:625`) *ends* by
calling `StartWorkers`, `StartScheduledSyncs`, `startSSEFanout`, `startEnvironmentWorkers`
and `startSharedLimits`. Constructing the service and starting its background loops were
the same function.

So a password reset claims jobs with a five-minute lease and then exits immediately,
leaving anything it claimed locked and unworked for up to five minutes. An operator runs
this command precisely when somebody is locked out — during an incident — which is the
worst moment to stall the job queue.

**Fix.** Handle maintenance flags before `openEnvironments`, or pass a flag that suppresses
listeners and workers.

---

## Recommended order

1. **F1, F2, F5** — one day, together. All three are the app lying about the state of the
   work. Highest trust recovered per line changed, exactly as the last audit's "honesty
   pass" was.
2. **F4** — the designer's palette and first-run guidance. The largest single gain for
   "people find it easy to use", and the one that needs design, not just code.
3. **F3, F8, F9** — make the work queue readable: mobile action, composed variables,
   legible urgency.
4. **F6, F7, F10, F11, F12** — naming, language and truncation. Cheap, and they compound.
5. **F16 into CI** — axe is already running; make it a gate so this class cannot regress.
6. **F13, F14** — delete hollow surface rather than disable it. A section that cannot act
   is better absent.

Split `DecisionEditor` (1228) and `TaskInbox` (1030) as the F1/F8 work touches them —
not as a separate refactor.

---

## Resolution — all findings applied, same day

Every finding above was fixed in one pass and verified in the browser against the seeded
sample data, not just in the diff. Two things found *while* fixing turned out to be worse
than what was reported:

**The Due Date column was fabricated.** F9 described a clipped `OVE…` badge. It was worse:
`TaskList.tsx` rendered the literal string `Today` and a red `Overdue` badge on **every
row**, whatever the task's actual due date — the same class as the invented dashboard
metrics the previous audit killed, and it had survived that pass. It now uses `urgencyOf`,
the computation the inbox already rates its rows with, so the two screens agree.

**"Batch Complete" had no `onClick`.** F15 argued it was the wrong thing to make the hero
action. It was also dead: a filled primary button wired to nothing. Removed rather than
disabled.

| # | State | Note |
| :-- | :-- | :-- |
| F1 | **Fixed** | Empty state now reads "Nothing is assigned to you yet — 12 tasks are waiting to be claimed", with a button that switches tabs. |
| F2 | **Fixed** | Binds `instancesData.needsAttentionTotal`, the same source the Instances page filters on. Dashboard now reads **4**, matching that page. |
| F3 | **Fixed** | `TaskCard` renders below `sm`; 12 Complete buttons reachable at 375px, no page overflow. |
| F4 | **Fixed** | "Start with a step" panel with a working **Add a step** button; palette button renamed "Add step"; Deploy Model disabled with a reason until the process has nodes and no blocking errors; React Flow watermark removed; the delete control's accessible name now matches its tooltip. |
| F5 | **Fixed** | `task.assignee?.username` or a dimmed "Unassigned". |
| F6 | **Fixed** | "All Tasks" matches the nav; Models titles itself by its active tab. |
| F7 | **Partly fixed — see below** | |
| F8 | **Fixed** | New tested domain module `domain/taskFacts.ts` (13 tests): composes amount + currency into **£1,750.00**, humanises keys, skips objects and the reference variable, bounds the count. Rendered as one line of values with the labels in a tooltip. |
| F9 | **Fixed** | Real due dates and urgency; badges are `light` (dark-on-tint) rather than white-on-accent. |
| F10 | **Fixed** | `textTransform: none`, `overflow: visible`, wrapping group — "v1 live" and "v4 staged" read in full. |
| F11 | **Fixed** | Counts distinct process keys; dashboard reads **2**, matching the Models page. |
| F12 | **Fixed** | New tested module `domain/signInError.ts` (8 tests). Unrecognised errors pass through with wrapping trimmed rather than being replaced by something reassuring and wrong. |
| F13 | **Fixed** | 2FA, API Keys, Clear Cache, Reset to Defaults and the Danger Zone removed. 2FA is now tracked in `security-plan.md` instead. |
| F14 | **Fixed** | Starter Templates section removed. |
| F15 | **Fixed** | Removed — and it was dead, not merely misplaced. |
| F16 | **Fixed** | The 13 unlabelled controls were `<TextInput type="checkbox">` — a text input wearing a checkbox type. Now real `Checkbox`es with names. The lint rule was widened to cover Mantine's form components, which surfaced 21 more across the designer and property panels; all 21 fixed. **0 remaining**, and CI now fails on a new one. |
| F17 | **Fixed** | `ScrollArea.Autosize mah={500}`. |
| F18 | **Fixed** | `padding-bottom: 96px` reserves the footer's space. |
| F19 | **Fixed** | `@fontsource/inter`, self-hosted, four weights. |

### F7 is extended, not closed

The switcher no longer translates only the navigation: all fifteen page headings and
standfirsts are in both catalogues, so the reported contradiction is gone — the heading now
reads **Dasbor** beside a nav that says **Dasbor**.

What is still English in Indonesian: stat card titles and hints, table column headers,
button labels, and timeline narratives. The machinery is right and the adoption is
partial. Finishing it is the `execution-plan.md` i18n item, and the pattern to follow is
now established in every page component.

### The backend bug

`setupService` no longer starts background work; a new `startBackgroundWork` is called from
`Run` only after the maintenance branch has had its chance to return. Verified: a
`--reset-password` run now logs **zero** worker or fan-out lines and still updates the
password.

`tests/maintenancemode` locks it in, in the style of `tests/endpointwiring` — a static
check that `setupService` calls none of the five starters, and that `Run` calls
`startBackgroundWork` *after* `handleResetPassword`. Confirmed to fail against the old
arrangement before it was fixed.

### What was deliberately not done

- **Splitting the god components.** `DecisionEditor` (1228) and `TaskInbox` (1030) are
  still too big. The F1/F8 work touched them without making them worse; splitting them is
  its own change with its own review.
- **The guided designer wizard and template gallery.** `roadmap.md` §7 has them as 🔴 High
  Priority. F4 makes an empty canvas answerable; it does not build the wizard.

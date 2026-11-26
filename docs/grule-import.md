# Importing Grule rules

Metis can take a rule set written for the [Grule rule
engine](https://github.com/hyperjumptech/grule-rule-engine) and turn it into
decision tables, plus a process that consults them. The goal is an exact
translation. A rule metis cannot reproduce exactly is left out, and the report
says why. It is never approximated.

## The call

```
POST /api/v1/decisions/import/grule
{
  "project_id": "…",
  "key": "pricing",                 // names the process and prefixes each table
  "name": "Pricing",                // the process's name, optional
  "files": [
    {"name": "discount.grl",  "content": "rule Gold … { … }"},
    {"name": "shipping.json", "content": "[{\"name\": \"FreeShipping\", …}]"}
  ],
  "preview": true,                  // report only, store nothing
  "deploy": false                   // also deploy the process
}
```

A file ending in `.json` is read as Grule JSON and a file ending in `.grl` as
GRL. Any other file is read by its first character. The reply is
`{"import": {...}}` with:

| Field | What it holds |
| :-- | :-- |
| `decisions` | The tables, in the order the process consults them. |
| `process` | The process: start, one business rule step per table, end. |
| `rules` | One entry per rule. An imported rule has `decision` (the table it is a line of) and sometimes `notes`. A rule left out has `reason`. |
| `results` | The process variable each field is written to, such as `Order.Discount` → `Order_Discount`. |
| `warnings` | Things said about the import as a whole. |
| `stored`, `process_id` | Set once the tables are stored and the process is deployed. |

Without `preview`, the tables are stored and go live. With `deploy` the
process is deployed too, in the same transaction. If any key is already used
in the project, the import is refused: an import never adds a version to a
policy that other processes consult. The caller needs the role that changes
models (designer, or releaser in a protected runtime).

## How rules become tables

Grule chains its rules. On each cycle it fires the matching rule with the
highest salience, and the facts that rule changes can make other rules match on
a later cycle. Metis decides in steps. Each step is one decision table. A rule
that reads a field another rule sets is decided in a later step than that rule.

| Grule | Metis |
| :-- | :-- |
| A rule | One line of a table. The line's id is the rule's name, and its description is the rule's description and file position. |
| `salience` | Line order. Rules of equal salience keep the order they were written in, and a warning says so, because Grule does not promise an order. |
| `Fact.Field` | Read from the process variable `Fact` (`Fact.Field` in FEEL). After a step sets the field, it is read from `Fact_Field`. |
| `Fact.Field op constant` in an `&&` condition | A cell in the `Fact.Field` column: `> 18`, `"gold"`, `!= null`. |
| Any other condition (`\|\|`, `!`, two fields compared) | A FEEL test in the *Other conditions* column. |
| `Fact.Field = constant` | A value in the output column. |
| `Fact.Field = expression` | A computed output column: the cell is FEEL, such as `(Order_Total * 0.2)`. |
| `+=`, `-=`, `*=`, `/=` | The same as `x = x op value`. |
| `Retract("SameRule")` | Accepted. A rule fires at most once. |
| `Log(...)` | Dropped. The rule's notes say so. |

Two shapes of rule set translate:

- **Rules that stop on a field they set.** For example,
  `when … && Order.Discount == 0 then Order.Discount = 20`. The first rule to
  fire sets the field and stops the others, so the table uses FIRST with lines
  in firing order. Every rule in the table has to set the same fields.
- **Rules that retract themselves.** Every matching rule fires, so the last
  rule to fire sets each field. Each set of fields gets one table that uses
  FIRST, with lines in reverse firing order.

These functions and methods translate: `Max`, `Min`, `Abs`, `Ceil`, `Floor`,
`Mod`, `Now`, `IsNil`, and on strings `Contains`, `HasPrefix`, `HasSuffix`,
`Len`, `ToUpper`, `ToLower` and `In(...)`.

## What is left out, and why

- **A rule that never stops.** It neither retracts itself nor tests a field it
  sets. Grule fires such a rule until its cycle limit.
- **A loop.** A sets what B reads, and B sets what A reads. That is inference,
  and a single pass cannot do it.
- **A rule whose answer depends on Grule's firing order.** This covers a field
  set in two different steps, rules that stop on a field and rules that
  retract themselves mixed on one field, and a rule that reads a field another
  rule in the same step sets.
- **Anything FEEL has no exact form for.** This covers other functions and
  methods, Go methods on facts, list and map elements (`[…]`), bitwise
  `&` and `|`, `Complete()`, `Changed()`, `Forget()`, and retracting a
  different rule.
- **A rule that reads what a rule left out would have set.** In Grule it would
  see that rule's answer. In metis nothing sets the field.

Some imported rules get a note:

- **A rule that shares a field with a rule left out.** Where the rule left out
  would have applied, metis answers differently from Grule.
- **A rule that divides.** Grule drops the remainder when both numbers are
  whole, and FEEL keeps it.
- **A rule that reads a field more than one rule sets.** Grule may fire the
  reader before the last of those rules has fired, but metis decides it after
  all of them. The import gives a warning.

## Safety

A rule file is untrusted input, just like a process definition. Metis parses it
with its own parser. Nothing in the file is run during the import. FEEL is
written from the parsed tree, with strings quoted, and never by pasting rule
text into an expression. The limits are:

- 1 MiB per file and 4 MiB per import
- 50 files and 2,000 rules
- 200 actions per rule
- 100 levels of nesting, where each operator in a chain such as `a + b + c` counts as a level

Each limit is reported by name when it is reached.

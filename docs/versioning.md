# Versioning and animal ranks

`revu` versions are named after the **Sumdog Animal Ranks** (rank #1 = Common
Rat, rank #31 = Baiji). The rank number is the version's minor number.

## Scheme

`MAJOR.MINOR.PATCH`

- **`x.0.x` — unnamed planning/documentation phase.** No animal name. Used for
  planning, ADRs, and docs that precede a named line.
- **`x.N.x` — rank #N.** For `N` in `1..31`, the minor version is the
  pseudoname at that rank: `1` = Common Rat, `2` = Common Chimpanzee, …,
  `31` = Baiji.
- **`x.N.P`** — patches within a named line. All work on a line lands as PRs on
  that line (e.g. every `1.1.x` change is a `1.1` PR) and is released as a patch.
- After `x.31.x` (Baiji) the line is exhausted: roll to `(x+1).0.x` (unnamed
  planning), then `(x+1).1.x` starts again at Common Rat.

So the sequence is `1.0` (plan) → `1.1` Common Rat → `1.2` Common Chimpanzee →
… → `1.31` Baiji → `2.0` (plan) → `2.1` Common Rat → …

## Ranks

| Minor | Pseudoname |
|---:|---|
| 1 | Common Rat |
| 2 | Common Chimpanzee |
| 3 | Narwhal |
| 4 | Harpy Eagle |
| 5 | Polar Bear |
| 6 | Indian Elephant |
| 7 | Okapi |
| 8 | Blue Whale |
| 9 | Clouded Leopard |
| 10 | Sumatran Orangutan |
| 11 | Yellow-crested Cockatoo |
| 12 | Cotton-top Tamarin |
| 13 | Baird's Tapir |
| 14 | Proboscis Monkey |
| 15 | Giant Otter |
| 16 | West Indian Manatee |
| 17 | Dhole |
| 18 | Mahogany Glider |
| 19 | Giant Panda |
| 20 | Red Slender Loris |
| 21 | Orinoco Crocodile |
| 22 | Lear's Macaw |
| 23 | Mountain Gorilla |
| 24 | Sumatran Tiger |
| 25 | Pygmy Three-toed Sloth |
| 26 | Golden-bellied Capuchin |
| 27 | Californian Condor |
| 28 | Wondiwoi Tree-kangaroo |
| 29 | Javan Rhinoceros |
| 30 | Panamanian Golden Frog |
| 31 | Baiji |

Notes: the source poster prints "Cokckatoo" and "Rhinocerous"; this table uses
the correct spellings.

## Milestones and tags

- A milestone is named `"<version> <Pseudoname> - <title>"`, e.g.
  `1.1 Common Rat - safe, deterministic pr review`.
- The unnamed planning phase uses `"<major>.0 - planning"`.
- Released tags follow `v<MAJOR>.<MINOR>.<PATCH>`, with a maintained major tag
  (`v1`) tracking the latest stable `1.x` release.

## Current position

- **1.0.x** (planning, unnamed) — the v1 build-out (Stages 0–6) and early
  hardening. Released: `v1.0.0`, `v1.0.1`.
- **1.1 Common Rat** — next; the post-v1 hardening sprint (PR #17, `1.1.x`)
  opens this line.

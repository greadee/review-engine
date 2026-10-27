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

## Release titles and purpose

The animal name is the release nickname. The release purpose is a separate theme
that appears in the release note.

- The tag and GitHub release title use the animal nickname:
  `v1.2.0 Common Chimpanzee`.
- The release note opens with the purpose: “This is the **Restore** release.”
- Do not use the purpose as the animal nickname, and do not title a release
  `v1.2.0 Restore`.

The same rule applies to patches: the tag and title carry the animal, while the
release note explains what the patch is for.

| Release | Animal nickname | Release purpose |
|---|---|---|
| `v1.2.0` | Common Chimpanzee | Restore |
| `v1.3.0` | Narwhal | Correctness & Security |
| `v1.4.0` | Harpy Eagle | Polish |
| `v1.5.0` | Polar Bear | Stability |

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

- A release milestone is named `"<version> <Pseudoname>"`; its description says
  `Release purpose: <theme>`. For example, `1.2 Common Chimpanzee` has the
  description “Release purpose: Restore”.
- Release titles follow the same convention: `v1.2.0 Common Chimpanzee`.
- The unnamed planning phase uses `"<major>.0 - planning"`.
- Released tags follow `v<MAJOR>.<MINOR>.<PATCH>`, with a maintained major tag
  (`v1`) tracking the latest stable `1.x` release.

## Current position

- **1.0.x** (planning, unnamed) — the v1 build-out (Stages 0–6) and early
  hardening. Released: `v1.0.0`, `v1.0.1`.
- **1.1 Common Rat** — post-v1 hardening. Released: `v1.1.1`.
- **1.2 Common Chimpanzee** — released as `v1.2.0`. Release purpose: Restore.
- **1.3 Narwhal** — planned. Release purpose: Correctness & Security.
- **1.4 Harpy Eagle** — planned. Release purpose: Polish.
- **1.5 Polar Bear** — planned. Release purpose: Stability.

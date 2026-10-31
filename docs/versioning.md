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
- **`x.N.0` — Named patch.** The rounded release for animal rank `N`. For
  example, Common Rat is `v1.1.0` and Common Chimpanzee is `v1.2.0`.
- **`x.N.P` (P > 0) — increment on the Named patch.** Patches on Common Rat are
  `v1.1.1`, `v1.1.2`, and so on. Patch numbers never start a new animal line.
- After `x.31.x` (Baiji) the line is exhausted: roll to `(x+1).0.x` (unnamed
  planning), then `(x+1).1.x` starts again at Common Rat.

So the sequence is `1.0` (plan) → `1.1.0` Common Rat → `1.2.0` Common
Chimpanzee → … → `1.31.0` Baiji → `2.0` (plan) → `2.1.0` Common Rat → …

Patch increments on a Named patch use the final number: `v1.1.0` → `v1.1.1`
→ `v1.1.2`. The 1.0.x tags predate the Named-patch rule and are retained as
historical planning releases.

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
| `v1.1.0` | Common Rat | Post-v1 hardening |
| `v1.1.1` | Common Rat | First increment on the Named patch (reserved) |
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
- **1.1 Common Rat** — Named patch `v1.1.0`; post-v1 hardening.
- **1.2 Common Chimpanzee** — released as `v1.2.0`. Release purpose: Restore.
- **1.3 Narwhal** — in progress on `1.3-correctness-security`. Release purpose:
  Correctness & Security. Sprint plan:
  [`docs/sprints/1.3.0-narwhal-correctness-security.md`](sprints/1.3.0-narwhal-correctness-security.md).
- **1.4 Harpy Eagle** — planned. Release purpose: Polish.
- **1.5 Polar Bear** — planned. Release purpose: Stability.

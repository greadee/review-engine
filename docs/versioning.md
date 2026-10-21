# Semantic versioning and release animals

## Semantic versioning

`revu` follows [Semantic Versioning 2.0.0](https://semver.org/).
Versions describe compatibility; animal names describe a release's WOW factor.

The public API includes the CLI commands, flags and exit-code meanings; GitHub
Action inputs and outputs; reusable workflow interfaces; supported configuration
keys and environment variables; and machine-readable findings, archived JSON and
review-bundle formats consumed by users. Go packages under `internal/` are
implementation details, but their serialized output can be a public interface.

For stable releases, use `MAJOR.MINOR.PATCH`:

| Change | Version increment |
|---|---|
| Incompatible change to an existing public interface | MAJOR; reset MINOR and PATCH to zero |
| Backward-compatible feature or public API addition, or public API deprecation | MINOR; reset PATCH to zero |
| Backward-compatible bug fix | PATCH |
| Documentation, chores or internal maintenance only | No release required; PATCH if included in a maintenance release |

Choose the highest required increment across the entire release. Internal
architecture changes may justify a minor release for substantial improvements;
they require a major release only when they break the public API. A bug-fix label
does not excuse an incompatible interface change. Correcting behavior to match an
existing documented contract is a bug fix; changing that contract requires the
appropriate compatibility increment.

- `1.0.0` establishes the initial stable public API. `x.0.x` is a normal release
  series, not a reserved planning or documentation phase.
- `0.y.z` is for initial development before the API is stable. Start at
  `0.1.0`; use minor increments for feature or incompatible development changes
  and patch increments for compatible fixes. Do not move an established stable
  product back to `0.x` to avoid its compatibility obligations.
- Prereleases use identifiers such as `1.2.0-alpha.1`, `1.2.0-beta.1`, and
  `1.2.0-rc.1`. They precede the corresponding stable release.
- Build metadata, such as `1.2.0+build.42`, does not affect version precedence.
- Released versions are immutable. Correct mistakes in a new release rather than
  moving or reusing a published full-version tag. Document migrations and mark
  superseded releases in their notes.
- Minor and patch numbers are not capped at 31. Animal scores never trigger a
  version increment, reset or major rollover.

## Release animals: WOW factor 1–31

Every release receives an integer WOW score from **1** (routine maintenance) to
**31** (exceptional, transformative work). Use the animal at that score in the
table below as the release nickname. The score reflects the release's uniqueness,
fun and visible impact, independently of its semantic version.

| WOW score | Typical release |
|---|---|
| 1–5 | Boring chores, documentation, dependency upkeep and routine maintenance |
| 6–10 | Useful fixes, polish and modest quality-of-life improvements |
| 11–15 | Simple UI changes, straightforward screens and small feature additions |
| 16–20 | Cool new features, substantial workflows and meaningful architectural improvements |
| 21–26 | Groundbreaking UI or interaction design, standout new capabilities and major architectural changes |
| 27–31 | Exceptional breakthroughs that substantially change what the product can do or how it feels |

Score the delivered release as a whole using its most significant changes, not
its commit count, effort, version number or marketing ambition. These bands are
judgment guides: a major architecture redesign should score high when it creates
meaningful new capabilities, while an invisible cleanup can score low. Simple UI
belongs in the middle-to-lower range; groundbreaking UI belongs in the
middle-to-upper range.

The release author proposes the score and a one-sentence rationale in the release
notes; the release reviewer confirms it. Reuse animals freely. Scores may rise or
fall between releases, and reaching Baiji does not restart the list or change the
major version. A breaking maintenance change can have a low WOW score and still
require a major bump; an exciting compatible feature can score high and remain a
minor release.

## Animal rankings

| WOW score | Animal |
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


## Release names, milestones and tags

- Full release tags use `vMAJOR.MINOR.PATCH`, with an optional SemVer prerelease
  suffix. Animal names and WOW scores stay out of the tag.
- Release titles use `v<version> — <Animal>: <short description>`.
- Release notes record `WOW: <score>/31 — <rationale>`, the compatibility
  classification, changes, validation and any migration instructions.
- Milestones may use `<version> <Animal> — <title>` once the version and score
  are known. Planning milestones can use descriptive names without inventing
  a release version.
- A moving major alias such as `v1` points to the latest reviewed, stable,
  compatible release in that major series. It is an Action convenience alias,
  not an immutable full-version release. Never point it at a prerelease or a
  release with known unresolved compatibility breaks. Full commit SHAs remain
  the reproducible pinning option.

Examples (illustrative, not declarations of published releases):

| Release | WOW | Why |
|---|---|---|
| `v1.2.1 — Common Rat: documentation cleanup` | 1 | Routine maintenance; patch if a release is needed |
| `v1.3.0 — Cotton-top Tamarin: simple results screen` | 12 | Compatible UI feature with modest novelty |
| `v1.4.0 — Sumatran Tiger: interactive review workspace` | 24 | Groundbreaking UI added without breaking existing interfaces |
| `v2.0.0 — Californian Condor: redesigned review protocol` | 27 | Transformative architecture with a documented incompatible protocol change |

## Existing releases and transition

Published releases include `v1.0.0`, `v1.0.1`, and `v1.1.1 Common Rat`.
Their versions and historical nicknames remain part of the release record; this
policy does not retroactively rescore or retag them.

The `v1.1.1` change renamed top-level archived JSON keys (for example,
`Findings` to `findings` and `BlockThreshold` to `blockThreshold`). Restore
the original output keys before promoting a corrected release through the
`v1` alias. With that compatibility restored, the accompanying additive
configuration features warrant a minor release; `v1.2.0` is the next proposed
version. Retaining the incompatible output instead would require a major release.
This policy update does not itself perform that code fix or publish a release.

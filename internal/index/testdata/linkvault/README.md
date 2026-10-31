# linkvault

Fixture vault for `TestFixtureEdges`. Open it in Obsidian to compare the graph view and the backlinks pane with the table below. Every link example here is inside code on purpose: this file is a note of the vault and must add no edges.

The expected edges were checked against the Obsidian app's metadata cache (`app.metadataCache.resolvedLinks`) on 2026-10-31. The `*Probe*` notes and the `Twin.md` files come from that check.

The test copies this folder to a temp dir and adds `nfd/Café.md` there, with the name in NFD, because git may normalize file names. It is not committed.

| Note | Link written | Edge |
| --- | --- | --- |
| `Home.md` | `[[Deep Note]]` | `deep/sub/Deep Note.md` (basename in a subfolder) |
| `Home.md` | `[[v1.2 notes]]` | `v1.2 notes.md` (dots in the name) |
| `Home.md` | `[[Ghost Target]]` | ghost `ghost target` |
| `CaseLinks.md` | `[[HOME]]`, `[[Home.MD]]` | `Home.md` (case, any-case extension; one edge) |
| `CaseLinks.md` | `[[deep note]]` | `deep/sub/Deep Note.md` |
| `CaseLinks.md` | `[[shout]]` | `Upper/Shout.MD` (upper-case extension) |
| `CaseLinks.md` | `[[ghost TARGET]]` | ghost `ghost target` (same ghost as Home) |
| `NFC.md` | `[[Café]]` typed in NFC | `nfd/Café.md` stored in NFD |
| `SelfLink.md` | `[[#Top]]`, `[[SelfLink]]` | `SelfLink.md` itself (one edge) |
| `Dups.md` | `[[Dup]]` | `a/Dup.md`, ambiguous: no same-folder match, tie on segments with `b/Dup.md`, `a` sorts first |
| `Dups.md` | `[[b/c/Dup]]` | `b/c/Dup.md`, not ambiguous (full path) |
| `SuffixProbe.md` | `[[c/Dup]]` | `b/c/Dup.md`, not ambiguous (path suffix, no root match) |
| `ProbeSuffix2.md` | `[[sub/Deep Note]]` | `deep/sub/Deep Note.md` (path suffix) |
| `b/Linker.md` | `[[Dup]]` | `b/Dup.md`, ambiguous: the same folder wins |
| `b/ProbeMdDup.md` | `[d](Dup.md)` | `b/Dup.md`, ambiguous: no root `Dup.md`, the same folder wins |
| `deep/sub/ProbeTie.md` | `[[Twin]]` | `deep/sub/Twin.md`, ambiguous: the same folder beats `q/Twin.md`, which has fewer segments |
| `deep/sub/ProbeTieMd.md` | `[t](Twin.md)` | `deep/sub/Twin.md`, ambiguous, as above |
| `md/ProbeWiki.md` | `[[Peer]]` | `Peer.md`: an exact path from the vault root beats `md/Peer.md` in the same folder |
| `md/ProbeDotSlash.md` | `[p](./Peer.md)` | `md/Peer.md`: `./` is relative to the note, with no fallback |
| `md/Forms.md` | `[plain](Peer.md)` | `Peer.md` at the root, as for `[[Peer]]`: markdown links are not relative unless they start with `./` or `../` |
| `md/Forms.md` | `[angle](<Spaced Note.md>)` | `md/Spaced Note.md` |
| `md/Forms.md` | `[encoded](Percent%20Note.md)` | `md/Percent Note.md` |
| `md/Forms.md` | `[up](../Home.md#Section)` | `Home.md` |
| `md/Forms.md` | `[rooted](/a/Dup.md)` | `a/Dup.md`, not ambiguous |
| `md/Forms.md` | `![image](../assets/pic.png)` | attachment `assets/pic.png` |
| `md/Forms.md` | `[out](../../outside.md)` | ghost `../../outside` (escapes the vault; ghost keys drop `.md`) |
| `md/Forms.md` | `[same note](#Forms)` | `md/Forms.md` itself |
| `md/Forms.md` | external URL | no edge |
| `Table.md` | `[[Deep Note\|alias]]` in a table cell | `deep/sub/Deep Note.md` |
| `Embeds.md` | `![[Home]]` | `Home.md` (embed) |
| `Embeds.md` | `![[pic.png]]`, `![[doc.pdf]]` | attachments `assets/pic.png`, `docs/doc.pdf` |
| `Embeds.md` | `![[missing.png]]` | ghost `missing.png` |
| `Front.md` | `[[Home]]`, `[[Deep Note]]` in frontmatter | `Home.md`, `deep/sub/Deep Note.md` |
| `Code.md` | `[[Home]]` plus links inside a fence and inline code | `Home.md` only |

Orphans: `Lonely.md`, `q/Twin.md` (it loses the tie) and this `README.md`.

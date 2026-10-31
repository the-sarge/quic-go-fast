# Domain docs

This repository uses a single-context layout: `GLOSSARY.md` at the repository root and architectural decision records in `docs/adr/`.

## Before exploring

Read root `GLOSSARY.md` and the ADRs relevant to the area being explored.

If these files do not exist, proceed silently without suggesting their creation upfront. The `domain-modeling` skill creates documentation lazily when terminology or decisions are resolved.

If a root `GLOSSARY-MAP.md` is introduced later, follow it to the relevant per-context `GLOSSARY.md` files and also read any applicable context-specific ADRs.

Before 2026-10-31 the glossary was `CONTEXT.md`. Frozen evidence, the development journal and pinned-revision links keep that name; read it as this glossary at the cited revision.

## Use the glossary's vocabulary

Use terms defined in `GLOSSARY.md` consistently in issues, proposals, hypotheses, and tests. Avoid synonyms the glossary explicitly rejects.

If a needed concept is absent, reconsider whether it belongs to the domain or note the gap for `domain-modeling`.

## Flag ADR conflicts

Explicitly identify any proposal that contradicts an existing ADR, name the ADR, and explain why reopening the decision may be justified.

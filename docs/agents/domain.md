# Domain docs

This repository uses a single-context layout: `CONTEXT.md` at the repository root and architectural decision records in `docs/adr/`.

## Before exploring

Read root `CONTEXT.md` and the ADRs relevant to the area being explored.

If these files do not exist, proceed silently without suggesting their creation upfront. The `domain-modeling` skill creates documentation lazily when terminology or decisions are resolved.

If a root `CONTEXT-MAP.md` is introduced later, follow it to the relevant context files and also read any applicable context-specific ADRs.

## Use the glossary's vocabulary

Use terms defined in `CONTEXT.md` consistently in issues, proposals, hypotheses, and tests. Avoid synonyms the glossary explicitly rejects.

If a needed concept is absent, reconsider whether it belongs to the domain or note the gap for `domain-modeling`.

## Flag ADR conflicts

Explicitly identify any proposal that contradicts an existing ADR, name the ADR, and explain why reopening the decision may be justified.

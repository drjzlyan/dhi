# F-063 — Knowledge retrieval by shared words (lexical vectors)

Status: done (M27 P7; the M2 "embedding retrieval (ADR-0007 seam)" deferral, scoped)

## Problem
The workspace knowledge base searched with ripgrep in fixed-string mode,
using the whole query as one phrase. "how do we deploy the api" found only
an entry containing exactly that phrase, which in practice means nothing.
A search tool that never finds anything teaches agents to stop using it.

## Behaviour
- Every published entry (title weighted twice, tags, body) and the query
  become sparse TF-IDF vectors over hashed word unigrams and bigrams.
  Matching words don't depend on order, plurals are folded, and common
  English stopwords are dropped. Entries are ranked by cosine similarity.
- Exact ripgrep matches still add evidence when a searcher is configured.
  Importance and freshness break ties.
- Matches below a similarity floor are dropped, so an unrelated question
  returns nothing rather than noise.
- A vector-only hit's snippet is the body line that shares the most
  query terms.
- Retrieval works without ripgrep: the KB no longer needs a binary to
  answer.

## Scope and naming
These are lexical vectors, not semantic embeddings: no model, nothing
downloaded, pure Go (ADR-0005). They find entries that share the
question's words, not its meaning ("ship" does not match "deploy"). The
`KnowledgeStore` seam (ADR-0007) is unchanged, so a model-backed store
can replace this later without touching callers.

## Acceptance
- [x] a natural-language question finds the right entry first without the
      phrase appearing verbatim; an unrelated query returns nothing; works
      with no searcher; snippet chosen for vector hits
- [x] tokenizer: stopwords dropped, plurals folded
- [x] existing KB and agent-runtime tests unchanged and green

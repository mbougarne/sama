# AI research and learning records

`agents/researches/` explains AI concepts relevant to understanding a request and producing a useful response. It follows the educational approach in the owner's Zigide examples. Provider/API investigations belong in normal project documentation and conversation evidence; they do not replace this learning record.

## What each record contains

Use the [research template](../../scripts/agent_collaboration/templates/research.md). Each input gets one record, with the exact filename of its conversation and input companions, containing:

1. **Epistemic Boundary:** distinguish observable facts from educational illustrations. State that the example is not model telemetry or private reasoning.
2. **Observable Context:** summarize the supplied request, relevant constraints, and sources actually inspected. Do not copy hidden instructions or invent an internal processing trace.
3. **AI Concept:** teach one relevant concept, such as tokenization, embeddings, context selection, attention, structured generation, tool feedback, uncertainty, or evaluation.
4. **Worked Example:** show a small calculation, runnable toy example, or concrete input/output transformation. Explain the variables and label all invented numbers. The example must teach mechanics, not decorate the document with AI vocabulary.
5. **Connection to This Exchange:** explain how the concept helps describe an observable part of this task at a high level. An analogy is not evidence that a specific mechanism produced this response.
6. **Limits:** state what is unobserved and what the example cannot establish.
7. **Sources:** cite inspected primary material or local examples; distinguish a local educational analogy from evidence about a model's architecture.
8. **Learning Experiment:** suggest a small change to the example and what the reader can check.

Record `concept` and `value_kind` (`observed`, `illustrative`, or `mixed`) in frontmatter. The body is limited to 100 lines. Prefer a different concept from the previous five research records. Even a short correction can teach instruction scope, evidence selection, or the difference between generation and validation; do not insert generic attention/vector boilerplate into every exchange.

## Accuracy boundary

Observable material includes the user's supplied text, files actually read, tool requests/results, edits, diagnostics, and the final response. Explain those at a useful high level. Exact model attention weights, hidden activations, logits, private chain-of-thought, and internal causal explanations are not available from ordinary repository tools. Do not fabricate them or label illustrative vectors as the real vectors used for the answer.

The linter enforces metadata, required sections, explicit labels, and a worked-example floor. It cannot certify an explanation's scientific correctness or establish which AI mechanism caused a response. Agents must review that distinction as carefully as code evidence.

## Inspected examples

The owner supplied three Zigide records inspected during adoption: `2026-08-16-15-23-46-434f2be2-a078-44ff-85bf-ac9fdec09c4b.md` (toy transformer/agent pipeline), `2026-08-23-17-54-40-3743d719-cade-41a5-9394-97bbae5aa30c.md` (structured generation and external audits), and `2026-08-29-11-32-36-57ba2dfc-eeaa-4684-b6a7-9da2100d8cc2.md` (shape versus literal metadata validation). This contract captures their educational intent; agents do not need that external checkout to follow it.

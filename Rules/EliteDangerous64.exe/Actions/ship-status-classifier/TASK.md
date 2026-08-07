# Elite Dangerous ship-status classifier

Consume one same-frame PP-OCR text-regions result. Confirm only the reviewed
prefixes `MASS`, `LANDING`, and `CARGO`; the remaining words are not required.
For each accepted label independently, inspect the bounded pixels immediately
to its left and return cyan `ON`, orange `OFF`, or evidence-preserving
`UNKNOWN`. Missing labels, malformed left context, and ambiguous colors never
fall back to the retired fixed-triplet detector.

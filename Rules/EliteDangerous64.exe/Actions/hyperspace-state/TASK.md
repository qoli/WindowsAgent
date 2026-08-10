# Read Elite Dangerous hyperspace visual state

Combine the current central flight-prompt OCR classification with a fresh
fixed cockpit HUD-presence sample. It reports `FSD_CHARGING` or
`ALIGNMENT_REQUIRED` when the corresponding OCR evidence is accepted;
otherwise it reports `COCKPIT_PRESENT` or `COCKPIT_ABSENT` from the reviewed
compass HUD region.

These are single-sample observations. `COCKPIT_ABSENT` is not itself a
hyperspace claim, and `COCKPIT_PRESENT` is not itself an arrival claim. An
owning Streaming Action must first confirm FSD charging, then require stable
absence followed by stable presence. No Journal, Status file, command history,
prior frame, or alternative region is substituted.

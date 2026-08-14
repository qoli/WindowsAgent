# Pure decision Action runtime

Status: Landed

`windows-pure-decision-v1` executes one Rule-owned, permission-free Starlark
package in the Agent process. It exists for bounded runtime-internal composition
where launching the observation Job Object would dominate the work, such as
classifying several OCR routes derived from one captured frame.

The runtime is deliberately narrow:

- the Action must be `internal`, finite, and return one schema-valid result;
- its package may declare no memory, file, or screen permissions and no native
  libraries;
- the ordinary Starlark step, wall-time, input-schema, output-schema, result,
  and log limits still apply;
- Observer, blob, native, Action, stream, input, and pointer operations are not
  available;
- callers must declare a static same-Rule dependency, and the Action checker
  includes that edge in missing, cross-Rule, self, and cycle validation.

This runtime does not replace `windows-observation-v1`. Any package that needs
Host resources, Windows isolation, the Observer protocol, or native libraries
continues to run in the bounded observation Job Object. A pure decision Action
is only a deterministic mapping over its declared JSON inputs.

The first consumer is the Elite Dangerous flight-prompt OCR cascade. The
generic OCR runtime owns one-frame image derivation and route execution; the
Rule-internal pure classifier remains the only owner of game phrases and
semantic thresholds.

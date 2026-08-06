# Windows Action Runtime

## Status

**Draft. No Windows input or mutating action capability is implemented.**

## Boundary

Each `action` package is finite, schema-bound, and owned by one executable
Rule. The action runner accepts only a declared action ID and schema-valid
logical inputs. It revalidates the exact foreground executable and revision,
serializes actions within the Game session, applies deadline/cancellation, and
releases all held input before returning.

An `action.requested` event must be durably committed before execution. The
runner appends started and one terminal succeeded, failed, rejected, or
cancelled event with matching correlation and causation. If the journal cannot
record the request or lifecycle, the action must not run.

The existing unauthenticated Windows capture/script HTTP surface is not an
action authorization boundary. Action invocation will require a separate local
authenticated contract before any input primitive is added.

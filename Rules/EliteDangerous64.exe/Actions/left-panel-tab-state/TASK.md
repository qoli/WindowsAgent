# Elite Dangerous left-panel tab state

Read four fixed `4x4` reference-coordinate samples and identify the active tab in
the four-state panel cycle: `SYSTEM`, `NAVIGATION`, `TRANSACTIONS`, or
`CONTACTS`. `SYSTEM` is the icon-only overview tab immediately left of
`NAVIGATION`.

The Action uses one calibrated square inside the filled orange highlight of
each tab. The three text-tab squares sit immediately beside their labels so
small header drift retains the same semantic anchor; SYSTEM remains inside its
icon tile. Exactly one square must meet the selected threshold while all other
squares remain below the inactive threshold. A missing header returns
`ABSENT` when every square contains at most four highlighted pixels out of 16;
this tolerates the calibrated cockpit-background noise observed while docked.
Any unselected square above that bound, or conflicting selected squares,
returns `UNKNOWN`. It does not OCR tab labels and does not infer state from a
previous invocation.

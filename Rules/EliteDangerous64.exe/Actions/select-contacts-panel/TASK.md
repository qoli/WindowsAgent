# Elite Dangerous select Contacts panel

This interruptible linear Streaming Action stops at one boundary: the left
panel is visible and its `CONTACTS` tab has been confirmed twice from the
current four-tab filled-highlight scan. It does not select a contact, focus
`REQUEST DOCKING`, or request docking.

The Action opens a stably absent panel at most once, then uses Frontier's
dedicated `CycleNextPanel` binding one step at a time. Every input is followed
by a settled, repeated header scan. Unknown evidence, a panel that does
not open, or failure to reach Contacts within three
cycles aborts explicitly; no previous frame or blind extra input is used.

The icon-only `SYSTEM` overview immediately left of `NAVIGATION` is one of the
four selectable states. They cycle in one exact order:
`SYSTEM -> NAVIGATION -> TRANSACTIONS -> CONTACTS -> SYSTEM`.

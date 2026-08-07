# Elite Dangerous select Contacts panel

This interruptible linear Streaming Action stops at one boundary: the Target
panel is visible and its `CONTACTS` tab has been confirmed twice from the
current fixed-region filled-highlight scan. It does not select a contact, focus
`REQUEST DOCKING`, or request docking.

The Action opens a stably absent panel at most once, then uses Frontier's
dedicated `CycleNextPanel` binding one step at a time. Every input is followed
by a settled, repeated CONTACTS-region scan. Unknown evidence, a panel that does
not open, or failure to reach Contacts within three
cycles aborts explicitly; no previous frame or blind extra input is used.

The icon-only `SYSTEM` summary Tag to the left of `NAVIGATION` is not one of
the selectable tabs. The four tabs cycle in one exact order:
`NAVIGATION -> TRANSACTIONS -> CONTACTS -> TARGET -> NAVIGATION`.

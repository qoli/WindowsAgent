# Close Elite Dangerous Navigation detail

This internal failure-compensation Action sends one BACK to leave an open
Navigation detail card, then requires two consecutive `NAVIGATION` tab-header
observations before invoking the verified left-panel closer. The detail card
itself hides those tab pixels and may be classified as `ABSENT`, so `ABSENT`
immediately after BACK is not proof that BACK worked. The Action succeeds only
after the returned Navigation list is independently closed and the child
reports `finalState=ABSENT`. It is not a general UI fallback.

# Ship heat

This finite composite returns the current forward-cockpit heat percentage from
the dedicated digit OCR and pure classifier. An explicit raw percent-form
reading below its confidence Gate remains `UNKNOWN`; the classifier does not
reinterpret a conflicting digits-only candidate as current heat. `UNKNOWN` never authorizes FSD
charging. The owning workflow selects its explicit maximum start temperature.

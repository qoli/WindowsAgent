# Commodity Market lower goods-column text regions

This finite resident PP-OCR Action reads the lower GOODS column of the
currently open Elite Dangerous Commodity Market. Its 450x456 reference ROI
starts exactly where `commodity-market-text-regions` ends, so callers can join
the two non-overlapping captures to cover every currently visible commodity
row. The lower slice stays within both the OCR pixel limit and the detection
runtime's 2048-pixel input-height limit on a native 4K frame.

It returns raw text regions and geometry only. It never identifies the current
Station or BUY/SELL mode, moves list focus, scrolls, selects a commodity, or
claims that a transaction occurred. Callers must combine it with the separate
header and upper-list observations and apply exact commodity semantics
themselves.

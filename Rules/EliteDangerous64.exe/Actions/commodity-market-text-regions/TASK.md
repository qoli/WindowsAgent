# Commodity Market text regions

Return raw PP-OCR text boxes from the visible Elite Dangerous Commodity Market
commodity list and trade dialog. The fixed reference ROI excludes the market
identity header and right-side comparison pane. The owning workflow combines
this output with the separate header OCR Action and accepts only an exact,
unique commodity name in the currently visible list.

This finite Action never chooses a commodity, changes tabs, scrolls, injects
input, interprets a completed trade, or reads Cargo.json.
